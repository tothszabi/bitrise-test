package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	envmanModels "github.com/bitrise-io/envman/models"
	"github.com/bitrise-io/go-utils/colorstring"
	"github.com/bitrise-io/go-utils/pointers"
	coreanalytics "github.com/bitrise-io/go-utils/v2/analytics"
	"github.com/gofrs/uuid"
	"github.com/tothszabi/bitrise-test/analytics"
	"github.com/tothszabi/bitrise-test/bitrise"
	"github.com/tothszabi/bitrise-test/configs"
	"github.com/tothszabi/bitrise-test/log"
	"github.com/tothszabi/bitrise-test/models"
	"github.com/tothszabi/bitrise-test/plugins"
	"github.com/tothszabi/bitrise-test/toolkits"
	"github.com/tothszabi/bitrise-test/tools"
	"github.com/tothszabi/bitrise-test/version"
	"github.com/urfave/cli"
)

const (
	// DefaultBitriseConfigFileName ...
	DefaultBitriseConfigFileName = "bitrise.yml"
	// DefaultSecretsFileName ...
	DefaultSecretsFileName = ".bitrise.secrets.yml"
	OutputFormatKey        = "output-format"

	depManagerBrew      = "brew"
	secretFilteringFlag = "secret-filtering"
)

var workflowNotSpecifiedErr = errors.New("workflow not specified")
var utilityWorkflowSpecifiedErr = errors.New("utility workflow specified")
var workflowRunFailedErr = errors.New("workflow run failed")

type RunConfig struct {
	Modes    models.WorkflowRunModes
	Config   models.BitriseDataModel
	Workflow string
	Secrets  []envmanModels.EnvironmentItemModel
}

var runCommand = cli.Command{
	Name:    "run",
	Aliases: []string{"r"},
	Usage:   "Runs a specified Workflow.",
	Action:  run,
	Flags: []cli.Flag{
		// cli params
		cli.StringFlag{Name: WorkflowKey, Usage: "workflow id to run."},
		cli.StringFlag{Name: ConfigKey + ", " + configShortKey, Usage: "Path where the workflow config file is located."},
		cli.StringFlag{Name: InventoryKey + ", " + inventoryShortKey, Usage: "Path of the inventory file."},
		cli.BoolFlag{Name: secretFilteringFlag, Usage: "Hide secret values from the log."},

		// cli params used in CI mode
		cli.StringFlag{Name: JSONParamsKey, Usage: "Specify command flags with json string-string hash."},
		cli.StringFlag{Name: JSONParamsBase64Key, Usage: "Specify command flags with base64 encoded json string-string hash."},
		cli.StringFlag{Name: OutputFormatKey, Usage: "Log format. Available values: json, console"},

		// deprecated
		flPath,

		// should deprecate
		cli.StringFlag{Name: ConfigBase64Key, Usage: "base64 encoded config data."},
		cli.StringFlag{Name: InventoryBase64Key, Usage: "base64 encoded inventory data."},
	},
}

func run(c *cli.Context) error {
	config, err := processArgs(c)
	if err != nil {
		if err == workflowNotSpecifiedErr {
			if config != nil {
				printAvailableWorkflows(config.Config)
			}
			failf("No workflow specified")
		} else if err == utilityWorkflowSpecifiedErr {
			printAboutUtilityWorkflowsText()
			failf("Utility workflows can't be triggered directly")
		}
		failf("Failed to process arguments: %s", err)
	}

	runner := NewWorkflowRunner(*config)
	exitCode, err := runner.RunWorkflowsWithSetupAndCheckForUpdate()
	if err != nil {
		if err == workflowRunFailedErr {
			msg := createWorkflowRunStatusMessage(exitCode)
			printWorkflowRunStatusMessage(msg)
			analytics.LogMessage("info", "bitrise-cli", "exit", map[string]interface{}{"build_slug": os.Getenv("BITRISE_BUILD_SLUG")}, msg)
			os.Exit(exitCode)
		}

		failf(err.Error())
	}

	msg := createWorkflowRunStatusMessage(0)
	printWorkflowRunStatusMessage(msg)
	analytics.LogMessage("info", "bitrise-cli", "exit", map[string]interface{}{"build_slug": os.Getenv("BITRISE_BUILD_SLUG")}, msg)
	os.Exit(0)

	return nil
}

type WorkflowRunner struct {
	config RunConfig
}

func NewWorkflowRunner(config RunConfig) WorkflowRunner {
	return WorkflowRunner{config: config}
}

func (r WorkflowRunner) RunWorkflowsWithSetupAndCheckForUpdate() (int, error) {
	if r.config.Workflow == "" {
		return 1, workflowNotSpecifiedErr
	}
	_, exist := r.config.Config.Workflows[r.config.Workflow]
	if !exist {
		return 1, fmt.Errorf("specified Workflow (%s) does not exist", r.config.Workflow)
	}

	tracker := analytics.NewDefaultTracker()
	defer func() {
		tracker.Wait()
	}()

	if err := bitrise.RunSetupIfNeeded(version.VERSION, false); err != nil {
		return 1, fmt.Errorf("setup failed: %s", err)
	}

	if buildRunResults, err := r.runWorkflows(tracker); err != nil {
		return 1, fmt.Errorf("failed to run workflow: %s", err)
	} else if buildRunResults.IsBuildFailed() {
		return buildRunResults.ExitCode(), workflowRunFailedErr
	}

	if err := checkUpdate(); err != nil {
		log.Warnf("failed to check for update, error: %s", err)
	}

	return 0, nil
}

func (r WorkflowRunner) runWorkflows(tracker analytics.Tracker) (models.BuildRunResultsModel, error) {
	startTime := time.Now()

	// Register run modes
	if err := registerRunModes(r.config.Modes); err != nil {
		return models.BuildRunResultsModel{}, fmt.Errorf("failed to register workflow run modes: %s", err)
	}

	targetWorkflow := r.config.Config.Workflows[r.config.Workflow]
	if targetWorkflow.Title == "" {
		targetWorkflow.Title = r.config.Workflow
	}

	// Envman setup
	if err := os.Setenv(configs.EnvstorePathEnvKey, configs.OutputEnvstorePath); err != nil {
		return models.BuildRunResultsModel{}, fmt.Errorf("failed to add env, err: %s", err)
	}

	if err := os.Setenv(configs.FormattedOutputPathEnvKey, configs.FormattedOutputPath); err != nil {
		return models.BuildRunResultsModel{}, fmt.Errorf("failed to add env, err: %s", err)
	}

	if err := tools.EnvmanInit(configs.OutputEnvstorePath, false); err != nil {
		return models.BuildRunResultsModel{}, fmt.Errorf("failed to run envman init: %s", err)
	}

	// App level environment
	environments := append(r.config.Secrets, r.config.Config.App.Environments...)

	if err := os.Setenv("BITRISE_TRIGGERED_WORKFLOW_ID", r.config.Workflow); err != nil {
		return models.BuildRunResultsModel{}, fmt.Errorf("failed to set BITRISE_TRIGGERED_WORKFLOW_ID env: %s", err)
	}
	if err := os.Setenv("BITRISE_TRIGGERED_WORKFLOW_TITLE", targetWorkflow.Title); err != nil {
		return models.BuildRunResultsModel{}, fmt.Errorf("failed to set BITRISE_TRIGGERED_WORKFLOW_TITLE env: %s", err)
	}

	environments = append(environments, targetWorkflow.Environments...)

	// Bootstrap Toolkits
	for _, aToolkit := range toolkits.AllSupportedToolkits() {
		toolkitName := aToolkit.ToolkitName()
		if !aToolkit.IsToolAvailableInPATH() {
			// don't bootstrap if any preinstalled version is available,
			// the toolkit's `PrepareForStepRun` can bootstrap for itself later if required
			// or if the system installed version is not sufficient
			if err := aToolkit.Bootstrap(); err != nil {
				return models.BuildRunResultsModel{}, fmt.Errorf("failed to bootstrap the required toolkit for the step (%s), error: %s",
					toolkitName, err)
			}
		}
	}

	// Trigger WillStartRun
	buildRunStartModel := models.BuildRunStartModel{
		EventName:   string(plugins.WillStartRun),
		StartTime:   startTime,
		ProjectType: r.config.Config.ProjectType,
	}
	if err := plugins.TriggerEvent(plugins.WillStartRun, buildRunStartModel); err != nil {
		log.Warnf("Failed to trigger WillStartRun, error: %s", err)
	}

	// Prepare workflow run parameters
	buildRunResults := models.BuildRunResultsModel{
		WorkflowID:     r.config.Workflow,
		StartTime:      startTime,
		StepmanUpdates: map[string]int{},
		ProjectType:    r.config.Config.ProjectType,
	}

	plan := createWorkflowRunPlan(r.config.Modes, r.config.Workflow, r.config.Config.Workflows, func() string { return uuid.Must(uuid.NewV4()).String() })
	if len(plan.ExecutionPlan) < 1 {
		return models.BuildRunResultsModel{}, fmt.Errorf("execution plan doesn't have any workflow to run")
	}

	buildIDProperties := coreanalytics.Properties{analytics.BuildExecutionID: uuid.Must(uuid.NewV4()).String()}

	log.PrintBitriseStartedEvent(plan)

	// Run workflows
	for i, workflowRunPlan := range plan.ExecutionPlan {
		isLastWorkflow := i == len(plan.ExecutionPlan)-1
		workflowToRun := r.config.Config.Workflows[workflowRunPlan.WorkflowID]
		if workflowToRun.Title == "" {
			workflowToRun.Title = workflowRunPlan.WorkflowID
		}
		buildRunResults = r.runWorkflow(workflowRunPlan, workflowRunPlan.WorkflowID, workflowToRun, r.config.Config.DefaultStepLibSource, buildRunResults, &environments, r.config.Secrets, isLastWorkflow, tracker, buildIDProperties)
	}

	// Build finished
	bitrise.PrintSummary(buildRunResults)

	// Trigger WorkflowRunDidFinish
	buildRunResults.EventName = string(plugins.DidFinishRun)
	if err := plugins.TriggerEvent(plugins.DidFinishRun, buildRunResults); err != nil {
		log.Warnf("Failed to trigger WorkflowRunDidFinish, error: %s", err)
	}

	return buildRunResults, nil
}

func processArgs(c *cli.Context) (*RunConfig, error) {
	workflowToRunID := c.String(WorkflowKey)
	if workflowToRunID == "" && len(c.Args()) > 0 {
		workflowToRunID = c.Args()[0]
	}

	var prGlobalFlagPtr *bool
	if c.GlobalIsSet(PRKey) {
		prGlobalFlagPtr = pointers.NewBoolPtr(c.GlobalBool(PRKey))
	}

	var ciGlobalFlagPtr *bool
	if c.GlobalIsSet(CIKey) {
		ciGlobalFlagPtr = pointers.NewBoolPtr(c.GlobalBool(CIKey))
	}

	var secretFiltering *bool
	if c.IsSet(secretFilteringFlag) {
		secretFiltering = pointers.NewBoolPtr(c.Bool(secretFilteringFlag))
	} else if os.Getenv(configs.IsSecretFilteringKey) == "true" {
		secretFiltering = pointers.NewBoolPtr(true)
	} else if os.Getenv(configs.IsSecretFilteringKey) == "false" {
		secretFiltering = pointers.NewBoolPtr(false)
	}

	var secretEnvsFiltering *bool
	if os.Getenv(configs.IsSecretEnvsFilteringKey) == "true" {
		secretEnvsFiltering = pointers.NewBoolPtr(true)
	} else if os.Getenv(configs.IsSecretEnvsFilteringKey) == "false" {
		secretEnvsFiltering = pointers.NewBoolPtr(false)
	}

	bitriseConfigBase64Data := c.String(ConfigBase64Key)
	bitriseConfigPath := c.String(ConfigKey)
	deprecatedBitriseConfigPath := c.String(PathKey)
	if bitriseConfigPath == "" && deprecatedBitriseConfigPath != "" {
		log.Warn("'path' key is deprecated, use 'config' instead!")
		bitriseConfigPath = deprecatedBitriseConfigPath
	}

	inventoryBase64Data := c.String(InventoryBase64Key)
	inventoryPath := c.String(InventoryKey)

	jsonParams := c.String(JSONParamsKey)
	jsonParamsBase64 := c.String(JSONParamsBase64Key)

	runParams, err := parseRunParams(
		workflowToRunID,
		bitriseConfigPath, bitriseConfigBase64Data,
		inventoryPath, inventoryBase64Data,
		jsonParams, jsonParamsBase64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse command params: %s", err)
	}

	if runParams.WorkflowToRunID == "" {
		return nil, workflowNotSpecifiedErr
	}
	if strings.HasPrefix(runParams.WorkflowToRunID, "_") {
		return nil, utilityWorkflowSpecifiedErr
	}

	inventoryEnvironments, err := CreateInventoryFromCLIParams(runParams.InventoryBase64Data, runParams.InventoryPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create inventory: %s", err)
	}

	bitriseConfig, warnings, err := CreateBitriseConfigFromCLIParams(runParams.BitriseConfigBase64Data, runParams.BitriseConfigPath)
	for _, warning := range warnings {
		log.Warnf("warning: %s", warning)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create bitrise config: %s", err)
	}

	isPRMode, err := isPRMode(prGlobalFlagPtr, inventoryEnvironments)
	if err != nil {
		return nil, fmt.Errorf("failed to check PR mode: %s", err)
	}

	isCIMode, err := isCIMode(ciGlobalFlagPtr, inventoryEnvironments)
	if err != nil {
		return nil, fmt.Errorf("failed to check CI mode: %s", err)
	}

	enabledFiltering, err := isSecretFiltering(secretFiltering, inventoryEnvironments)
	if err != nil {
		return nil, fmt.Errorf("failed to check Secret Filtering mode: %s", err)
	}

	enabledEnvsFiltering, err := isSecretEnvsFiltering(secretEnvsFiltering, inventoryEnvironments)
	if err != nil {
		return nil, fmt.Errorf("failed to check Secret Envs Filtering mode: %s", err)
	}

	noOutputTimeout := readNoOutputTimoutConfiguration(inventoryEnvironments)

	return &RunConfig{
		Modes: models.WorkflowRunModes{
			CIMode:                  isCIMode,
			PRMode:                  isPRMode,
			DebugMode:               configs.IsDebugMode,
			NoOutputTimeout:         noOutputTimeout,
			SecretFilteringMode:     enabledFiltering,
			SecretEnvsFilteringMode: enabledEnvsFiltering,
		},
		Config:   bitriseConfig,
		Workflow: runParams.WorkflowToRunID,
		Secrets:  inventoryEnvironments,
	}, nil
}

func registerRunModes(modes models.WorkflowRunModes) error {
	if err := registerCIMode(modes.CIMode); err != nil {
		return fmt.Errorf("failed to register CI mode: %s", err)
	}

	if err := registerPrMode(modes.PRMode); err != nil {
		return fmt.Errorf("failed to register PR mode: %s", err)
	}

	if err := registerSecretFiltering(modes.SecretFilteringMode); err != nil {
		return fmt.Errorf("failed to register Secret Filtering mode: %s", err)
	}

	if err := registerSecretEnvsFiltering(modes.SecretEnvsFilteringMode); err != nil {
		return fmt.Errorf("failed to register Secret Envs Filtering mode: %s", err)
	}

	return nil
}

func createWorkflowRunPlan(modes models.WorkflowRunModes, targetWorkflow string, workflows map[string]models.WorkflowModel, uuidProvider func() string) models.WorkflowRunPlan {
	var executionPlan []models.WorkflowExecutionPlan
	workflowList := walkWorkflows(targetWorkflow, workflows, nil)
	for _, workflowID := range workflowList {
		workflow := workflows[workflowID]

		var stepPlan []models.StepExecutionPlan
		for _, stepItem := range workflow.Steps {
			stepID, _ := stepItem.GetStepIDAndStep()
			stepPlan = append(stepPlan, models.StepExecutionPlan{
				UUID:   uuidProvider(),
				StepID: stepID,
			})
		}

		executionPlan = append(executionPlan, models.WorkflowExecutionPlan{
			UUID:       uuidProvider(),
			WorkflowID: workflowID,
			Steps:      stepPlan,
		})
	}

	cliVersion := version.VERSION
	if version.IsAlternativeInstallation {
		cliVersion = fmt.Sprintf("%s (%s)", cliVersion, version.Commit)
	}

	return models.WorkflowRunPlan{
		Version:                 cliVersion,
		LogFormatVersion:        "1",
		CIMode:                  modes.CIMode,
		PRMode:                  modes.PRMode,
		DebugMode:               modes.DebugMode,
		NoOutputTimeoutMode:     modes.NoOutputTimeout > 0,
		SecretFilteringMode:     modes.SecretFilteringMode,
		SecretEnvsFilteringMode: modes.SecretEnvsFilteringMode,
		ExecutionPlan:           executionPlan,
	}
}

func walkWorkflows(workflowID string, workflows map[string]models.WorkflowModel, workflowStack []string) []string {
	workflow := workflows[workflowID]
	for _, before := range workflow.BeforeRun {
		workflowStack = walkWorkflows(before, workflows, workflowStack)
	}

	workflowStack = append(workflowStack, workflowID)

	for _, after := range workflow.AfterRun {
		workflowStack = walkWorkflows(after, workflows, workflowStack)
	}

	return workflowStack
}

func printAvailableWorkflows(config models.BitriseDataModel) {
	workflowNames := []string{}
	utilityWorkflowNames := []string{}

	for wfName := range config.Workflows {
		if strings.HasPrefix(wfName, "_") {
			utilityWorkflowNames = append(utilityWorkflowNames, wfName)
		} else {
			workflowNames = append(workflowNames, wfName)
		}
	}
	sort.Strings(workflowNames)
	sort.Strings(utilityWorkflowNames)

	if len(workflowNames) > 0 {
		log.Print("The following workflows are available:")
		for _, wfName := range workflowNames {
			log.Print(" * " + wfName)
		}

		log.Print()
		log.Print("You can run a selected workflow with:")
		log.Print("$ bitrise run WORKFLOW-ID")
		log.Print()
	} else {
		log.Print("No workflows are available!")
	}

	if len(utilityWorkflowNames) > 0 {
		log.Print()
		log.Print("The following utility workflows are defined:")
		for _, wfName := range utilityWorkflowNames {
			log.Print(" * " + wfName)
		}

		log.Print()
		printAboutUtilityWorkflowsText()
		log.Print()
	}
}

func printAboutUtilityWorkflowsText() {
	log.Print("Note about utility workflows:")
	log.Print(" Utility workflow names start with '_' (example: _my_utility_workflow).")
	log.Print(" These workflows can't be triggered directly, but can be used by other workflows")
	log.Print(" in the before_run and after_run lists.")
}

func printWorkflowRunStatusMessage(msg string) {
	log.Print()
	log.Print(msg)
	log.Print()
}

func createWorkflowRunStatusMessage(exitCode int) string {
	var message string
	var colorMessage string
	if exitCode == 0 {
		message = "Bitrise build successful"
		colorMessage = colorstring.Green(message)
	} else {
		message = fmt.Sprintf("Bitrise build failed (exit code: %d)", exitCode)
		colorMessage = colorstring.Red(message)
	}
	return colorMessage
}
