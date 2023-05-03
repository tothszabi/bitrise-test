package version

import (
	"runtime/debug"
	"strings"
)

func init() {
	// Goreleaser fills out the version field if the regular release process is followed.
	if VERSION != "" {
		return
	}

	// If the binary was installed with `go install` then we will have build information.
	info, available := debug.ReadBuildInfo()
	if available && info.Main.Version != "" {
		// Go needs to have a `v` prefix before the version but that is incompatible with the CLI versioning.
		VERSION, _ = strings.CutPrefix(info.Main.Version, "v")
	} else {
		VERSION = "0.0.0"
	}
}
