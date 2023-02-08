package cli

import (
	"github.com/tothszabi/bitrise-test/tools"
	"github.com/urfave/cli"
)

func share(c *cli.Context) error {
	if err := tools.StepmanShare(); err != nil {
		failf("Bitrise share failed, error: %s", err)
	}

	return nil
}
