package version

import (
	"runtime/debug"
	"strings"
)

var IsAlternativeInstallation = false

func init() {
	// If the binary was installed with `go install` then we will have build information.
	info, available := debug.ReadBuildInfo()
	if available && info.Main.Version != "" {
		IsAlternativeInstallation = true

		components := strings.Split(info.Main.Version, "-")
		count := len(components)
		if count != 0 {
			Commit = components[count-1]
		}
	}
}
