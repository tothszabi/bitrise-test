package cli

import (
	"fmt"

	"github.com/tothszabi/bitrise-test/bitrise"
	"github.com/tothszabi/bitrise-test/log"
	"github.com/tothszabi/bitrise-test/plugins"
	"github.com/tothszabi/bitrise-test/version"
	"github.com/urfave/cli"
)

var initCmd = cli.Command{
	Name:    "init",
	Aliases: []string{"i"},
	Usage:   "Init bitrise config.",
	Action: func(c *cli.Context) error {
		if err := initConfig(c); err != nil {

			// If the plugin is not installed yet run the bitrise setup first and try it again
			perr, ok := err.(plugins.NotInstalledError)
			if ok {
				log.Warn(perr)
				log.Print("Running setup to install the default plugins")
				log.Print()

				if err := bitrise.RunSetup(version.VERSION, false, false); err != nil {
					return fmt.Errorf("Setup failed, error: %s", err)
				}

				if err := initConfig(c); err != nil {
					failf(err.Error())
				}
			} else {
				failf(err.Error())
			}
		}
		return nil
	},
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:  "minimal",
			Usage: "creates empty bitrise config and secrets",
		},
	},
}

func initConfig(c *cli.Context) error {
	minimal := c.Bool("minimal")

	pluginName := "init"
	plugin, found, err := plugins.LoadPlugin(pluginName)
	if err != nil {
		return fmt.Errorf("Failed to get plugin (%s), error: %s", pluginName, err)
	}

	if !found {
		return plugins.NewNotInstalledError("init")
	}

	pluginArgs := []string{}
	if minimal {
		pluginArgs = []string{"--minimal"}
	}
	if err := plugins.RunPluginByCommand(plugin, pluginArgs); err != nil {
		return fmt.Errorf("Failed to run plugin (%s), error: %s", pluginName, err)
	}

	return nil
}
