package cli

import (
	"github.com/tothszabi/bitrise-test/tools"
	"github.com/urfave/cli"
)

func finish(c *cli.Context) error {
	if err := tools.StepmanShareFinish(); err != nil {
		failf("Bitrise share finish failed, error: %s", err)
	}

	return nil
}
