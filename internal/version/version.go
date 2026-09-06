// Package version exposes build metadata stamped in at link time.
package version

import "runtime/debug"

// Version is overridden with -ldflags at release time; "dev" in local builds.
var Version = "dev"

// Revision returns the VCS revision recorded by the Go toolchain, or "unknown".
func Revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return "unknown"
}
