package corelog

import (
	"fmt"
	"io"
	"strings"

	"github.com/bitrise-io/bitrise/models"
	"github.com/bitrise-io/colorstring"
)

var levelToANSIColorCode = map[Level]ANSIColorCode{
	ErrorLevel: RedCode,
	WarnLevel:  YellowCode,
	InfoLevel:  BlueCode,
	DoneLevel:  GreenCode,
	DebugLevel: MagentaCode,
}

type ANSIColorCode string

const (
	RedCode     ANSIColorCode = "\x1b[31;1m"
	YellowCode  ANSIColorCode = "\x1b[33;1m"
	BlueCode    ANSIColorCode = "\x1b[34;1m"
	GreenCode   ANSIColorCode = "\x1b[32;1m"
	MagentaCode ANSIColorCode = "\x1b[35;1m"
	ResetCode   ANSIColorCode = "\x1b[0m"
)

type consoleLogger struct {
	output io.Writer
}

func newConsoleLogger(output io.Writer) *consoleLogger {
	return &consoleLogger{
		output: output,
	}

}

// LogMessage ...
func (l *consoleLogger) LogMessage(message string, fields MessageLogFields) {
	message = addColor(fields.Level, message)

	var prefixes []string
	if fields.Timestamp != "" {
		prefixes = append(prefixes, fmt.Sprintf("[%s]", fields.Timestamp))
	}
	if fields.Producer != "" {
		prefixes = append(prefixes, string(fields.Producer))
	}
	if fields.ProducerID != "" {
		prefixes = append(prefixes, fields.ProducerID)
	}
	prefix := strings.Join(prefixes, " ")
	if prefix != "" && message != "" {
		prefix += " "
	}

	message = prefix + message
	if _, err := fmt.Fprint(l.output, message); err != nil {
		// Encountered an error during writing the message to the output. Manually construct a message for
		// the error and print it to the stdout.
		fmt.Printf("writing log message failed: %s", err)
	}
}

func (l consoleLogger) LogEvent(content interface{}, fields EventLogFields) {
	switch fields.EventType {
	case "bitrise_started":
		plan, ok := content.(models.WorkflowRunPlan)
		if !ok {
			fmt.Printf("writing event message failed: (%#v) is not a workflow run plan", content)
		}
		l.LogBitriseStartedEvent(plan)
	default:
		fmt.Printf("writing event message failed: unkown event: %v", fields.EventType)
	}
}

func (l consoleLogger) LogBitriseStartedEvent(plan models.WorkflowRunPlan) {
	messages := createBitriseStartedMessages(plan)
	for _, message := range messages {
		l.LogMessage(message.Message, MessageLogFields{
			Level: message.Level,
		})
	}
}

type MessageWithLevel struct {
	Message string
	Level   Level
}

func createBitriseStartedMessages(plan models.WorkflowRunPlan) []MessageWithLevel {
	var messages []MessageWithLevel
	messages = append(messages, MessageWithLevel{
		Message: `
██████╗ ██╗████████╗██████╗ ██╗███████╗███████╗
██╔══██╗██║╚══██╔══╝██╔══██╗██║██╔════╝██╔════╝
██████╔╝██║   ██║   ██████╔╝██║███████╗█████╗
██╔══██╗██║   ██║   ██╔══██╗██║╚════██║██╔══╝
██████╔╝██║   ██║   ██║  ██║██║███████║███████╗
╚═════╝ ╚═╝   ╚═╝   ╚═╝  ╚═╝╚═╝╚══════╝╚══════╝
`,
		Level: NormalLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("version: %s\n", colorstring.Green(plan.Version)),
		Level:   NormalLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: "\n",
		Level:   NormalLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("CI mode: %v\n", plan.CIMode),
		Level:   WarnLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("PR mode: %v\n", plan.PRMode),
		Level:   WarnLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("Debug mode: %v\n", plan.DebugMode),
		Level:   WarnLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("Secret filtering mode: %v\n", plan.SecretFilteringMode),
		Level:   WarnLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("Secret Envs filtering mode: %v\n", plan.SecretEnvsFilteringMode),
		Level:   WarnLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("No output timeout mode: %v\n", plan.NoOutputTimeoutMode),
		Level:   WarnLevel,
	})
	messages = append(messages, MessageWithLevel{
		Message: "\n",
		Level:   NormalLevel,
	})

	var workflowIDs []string
	for _, workflowPlan := range plan.ExecutionPlan {
		workflowID := workflowPlan.WorkflowID
		if workflowPlan.WorkflowID == plan.TargetWorkflowID {
			workflowID = colorstring.Green(workflowPlan.WorkflowID)
		}
		workflowIDs = append(workflowIDs, workflowID)
	}
	var prefix string
	if len(workflowIDs) == 1 {
		prefix = colorstring.Blue("Running workflow")
	} else {
		prefix = colorstring.Blue("Running workflows")
	}
	messages = append(messages, MessageWithLevel{
		Message: fmt.Sprintf("%s: %s\n", prefix, strings.Join(workflowIDs, " -->  ")),
		Level:   NormalLevel,
	})
	return messages
}

func addColor(level Level, message string) string {
	if message == "" {
		return message
	}

	color := levelToANSIColorCode[level]
	if color != "" {
		return string(color) + message + string(ResetCode)
	}
	return message
}
