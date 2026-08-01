// Package buildinfo answers one question — which build is this? — from
// whatever the toolchain happened to record.
//
// A release binary is stamped by the linker. A `go install pkg@v0.1.3` binary
// is not, but the module system knows its version. A binary built from a
// working copy has neither, yet Go stamps the commit it came from. The
// cascade below prefers the most authoritative of those three, and never
// reports a version it cannot support.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Fallback is what a build reports when nothing identifies it: no linker
// stamp, no module version, no VCS data.
const Fallback = "dev"

// devel is what the module system reports for a build that did not come from
// a resolved module version.
const devel = "(devel)"

// shortRevisionLen is how much of a commit hash a human reads.
const shortRevisionLen = 7

// dirtySuffix marks a build made from a modified working tree.
const dirtySuffix = "+dirty"

// Info is a build's identity.
type Info struct {
	// Version is what to show: a tag, a module version, a short commit, or
	// Fallback — suffixed with +dirty when the tree was modified.
	Version string
	// Revision is the commit, shortened. Empty when the build carries no VCS
	// stamp, which is the case inside a container that copied a source tree
	// without its .git.
	Revision string
	// Time is when the commit was made, RFC3339 as Go records it.
	Time string
	// Modified reports an uncommitted working tree at build time.
	Modified  bool
	GoVersion string
	Platform  string
}

// Resolve reports the build's identity. linkerVersion is the value the linker
// stamped (`-ldflags "-X main.version=..."`); pass "" when there is none.
func Resolve(linkerVersion string) Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(linkerVersion, bi, ok)
}

// resolve is Resolve with its one dependency injected, so the cascade can be
// tested at every step rather than only at whichever one this binary took.
func resolve(linkerVersion string, bi *debug.BuildInfo, ok bool) Info {
	info := Info{
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}

	var moduleVersion string
	if ok && bi != nil {
		moduleVersion = bi.Main.Version
		if bi.GoVersion != "" {
			info.GoVersion = bi.GoVersion
		}
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				info.Revision = shorten(setting.Value)
			case "vcs.time":
				info.Time = setting.Value
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}

	// The cascade, most authoritative first. The linker stamp wins because it
	// is the only one that carries the tag a release was cut from: a module
	// version can be absent and a commit says nothing about which release it
	// became.
	switch {
	case linkerVersion != "":
		info.Version = linkerVersion
	case moduleVersion != "" && moduleVersion != devel:
		info.Version = moduleVersion
	case info.Revision != "":
		// A local build of a checkout: the commit is the only true answer.
		info.Version = info.Revision
	default:
		info.Version = Fallback
	}

	// +dirty is a fact about the artifact, not about where its version came
	// from, so it is appended whichever branch above ran. A stamped release
	// built from an unclean tree is exactly the case worth flagging.
	//
	// The module system stamps its own +dirty on Main.Version, so this checks
	// before appending: a local build of a tagged, modified tree otherwise
	// reports v0.1.3+dirty+dirty.
	if info.Modified && !strings.HasSuffix(info.Version, dirtySuffix) {
		info.Version += dirtySuffix
	}
	return info
}

// String is the one-line form for `--version`.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString("pix-sandbox ")
	b.WriteString(i.Version)
	if i.Revision != "" {
		b.WriteString(" (")
		b.WriteString(i.Revision)
		if i.Time != "" {
			b.WriteString(", ")
			b.WriteString(i.Time)
		}
		b.WriteString(")")
	}
	b.WriteString("\n")
	b.WriteString(i.GoVersion)
	b.WriteString(" ")
	b.WriteString(i.Platform)
	return b.String()
}

func shorten(revision string) string {
	if len(revision) <= shortRevisionLen {
		return revision
	}
	return revision[:shortRevisionLen]
}
