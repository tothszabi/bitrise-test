package cli

import (
	"github.com/tothszabi/bitrise-test/tools"
	"github.com/urfave/cli"
)

func shareAudit(c *cli.Context) error {
	if err := tools.StepmanShareAudit(); err != nil {
		failf("Bitrise share audit failed, error: %s", err)
	}

	return nil
}
