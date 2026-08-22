// Package buildinfo reports which build of the binary is running.
//
// A local dashboard that people leave open for weeks is a dashboard whose
// version somebody eventually has to ask about, usually while explaining a
// number. Every build can answer, not only the tagged ones: a release is
// stamped, `go install` carries a module version, and a plain `go build`
// still knows the commit it came from.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// version is stamped by the release build:
//
//	-ldflags "-X github.com/billyham07/harness-observer/internal/buildinfo.version=v0.1.0"
//
// It is deliberately unexported: Version() is the only way to read it, so the
// fallbacks below cannot be skipped by accident.
var version string

// Version returns the release tag for a stamped build, the module version for
// one installed with `go install`, or a short commit for a local build. It
// never returns an empty string, because "unknown version" is a worse answer
// than "dev".
func Version() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var revision string
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	// A dirty tree says so. A build from uncommitted changes that reports a
	// clean commit is the version string nobody can act on.
	if dirty {
		return "dev+" + revision + ".dirty"
	}
	return "dev+" + revision
}

// Platform is the toolchain and target this binary was built for.
func Platform() string {
	return fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
