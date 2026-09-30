package meta

import (
	"runtime"
	"runtime/debug"
)

// AppName is the program name used for the binary, XDG directories, the
// debug log and user-facing messages.
const AppName = "sdash"

// Build identity must remain package-level strings for linker overrides.
var (
	// Version is the release version, e.g. "v0.1.0".
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
	// Date is the build timestamp in RFC 3339 form.
	Date = "unknown"
	// Build says how the binary was built: "release" (GoReleaser),
	// "make" (a git checkout), or "" (plain go build).
	Build = ""
)

// BuildInfo describes the running binary.
type BuildInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	Build     string `json:"build,omitempty"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Info returns the build identity. When the binary was built without
// ldflags (for example with plain "go build" in a git checkout), commit and
// date fall back to the VCS stamp the Go toolchain embeds.
func Info() BuildInfo {
	info := BuildInfo{
		Name:      AppName,
		Version:   Version,
		Commit:    Commit,
		Date:      Date,
		Build:     Build,
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		applyVCS(&info, bi.Settings)
	}
	return info
}

// applyVCS fills commit and date from embedded VCS settings when ldflags did
// not set them. A dirty working tree is marked with a "-dirty" suffix.
func applyVCS(info *BuildInfo, settings []debug.BuildSetting) {
	var revision, vcsTime string
	dirty := false
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			vcsTime = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if info.Commit == "none" && revision != "" {
		info.Commit = revision
		if dirty {
			info.Commit += "-dirty"
		}
	}
	if info.Date == "unknown" && vcsTime != "" {
		info.Date = vcsTime
	}
}

// Repo is the GitHub repository that publishes releases.
const Repo = "nbharathik/slurm-dashboard"

// IssuesURL is where users report bugs; crash messages point here.
const IssuesURL = "https://github.com/" + Repo + "/issues"
