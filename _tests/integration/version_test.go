package integration

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/bitrise-io/go-utils/command"
	"github.com/stretchr/testify/require"
	"github.com/tothszabi/bitrise-test/version"
)

func Test_VersionOutput(t *testing.T) {
	t.Log("Version --full")
	{
		out, err := command.RunCommandAndReturnCombinedStdoutAndStderr(binPath(), "version", "--full")
		require.NoError(t, err)

		expectedOSVersion := fmt.Sprintf("%s (%s)", runtime.GOOS, runtime.GOARCH)
		expectedVersionOut := fmt.Sprintf(`version: %s
format version: 12
os: %s
go: %s
build number: 
commit:`, version.Version, expectedOSVersion, runtime.Version())

		require.Equal(t, expectedVersionOut, out)
	}
}
