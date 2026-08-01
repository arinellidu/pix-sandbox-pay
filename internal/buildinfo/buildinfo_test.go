package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

// stamped builds the BuildInfo the toolchain would hand back for a given
// module version and VCS state.
func stamped(moduleVersion, revision, when string, modified bool) *debug.BuildInfo {
	bi := &debug.BuildInfo{GoVersion: "go1.26.5"}
	bi.Main.Version = moduleVersion
	if revision != "" {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: "vcs.revision", Value: revision})
	}
	if when != "" {
		bi.Settings = append(bi.Settings, debug.BuildSetting{Key: "vcs.time", Value: when})
	}
	bi.Settings = append(bi.Settings, debug.BuildSetting{
		Key: "vcs.modified", Value: map[bool]string{true: "true", false: "false"}[modified],
	})
	return bi
}

const (
	fullRevision = "17b44c1e9d3f0a2b5c8e1d4f7a0b3c6e9d2f5a81"
	shortRev     = "17b44c1"
	commitTime   = "2026-08-01T14:20:00Z"
)

// The linker stamp is the only source that knows which tag a release was cut
// from, so it outranks everything the module system reports.
func TestLinkerVersionOverridesBuildInfo(t *testing.T) {
	got := resolve("v0.1.4", stamped("v0.1.3", fullRevision, commitTime, false), true)

	if got.Version != "v0.1.4" {
		t.Errorf("Version = %q, want the linker's v0.1.4", got.Version)
	}
	// Precedence decides the version, it does not discard the VCS facts.
	if got.Revision != shortRev {
		t.Errorf("Revision = %q, want %q", got.Revision, shortRev)
	}
	if got.Time != commitTime {
		t.Errorf("Time = %q, want %q", got.Time, commitTime)
	}
}

// `go install pkg@v0.1.3` leaves no linker stamp; the module version is then
// the authoritative answer.
func TestModuleVersionWhenNoLinkerStamp(t *testing.T) {
	got := resolve("", stamped("v0.1.3", fullRevision, commitTime, false), true)

	if got.Version != "v0.1.3" {
		t.Errorf("Version = %q, want the module's v0.1.3", got.Version)
	}
}

// A build from a working copy reports (devel); the commit is then the only
// true answer, and it is reported short.
func TestRevisionWhenModuleVersionIsDevel(t *testing.T) {
	got := resolve("", stamped(devel, fullRevision, commitTime, false), true)

	if got.Version != shortRev {
		t.Errorf("Version = %q, want the short revision %q", got.Version, shortRev)
	}
	if len(got.Revision) != shortRevisionLen {
		t.Errorf("Revision = %q, want %d characters", got.Revision, shortRevisionLen)
	}
}

// dev is the last resort: no linker stamp, no usable module version, no VCS.
func TestFallbackOnlyWhenEverythingIsAbsent(t *testing.T) {
	t.Run("no build info at all", func(t *testing.T) {
		if got := resolve("", nil, false); got.Version != Fallback {
			t.Errorf("Version = %q, want %q", got.Version, Fallback)
		}
	})

	t.Run("build info without version or vcs", func(t *testing.T) {
		if got := resolve("", stamped(devel, "", "", false), true); got.Version != Fallback {
			t.Errorf("Version = %q, want %q", got.Version, Fallback)
		}
	})

	// Anything present at all must displace the fallback.
	for name, bi := range map[string]*debug.BuildInfo{
		"module version": stamped("v0.1.3", "", "", false),
		"revision only":  stamped(devel, fullRevision, "", false),
	} {
		t.Run("not "+name, func(t *testing.T) {
			if got := resolve("", bi, true); got.Version == Fallback {
				t.Errorf("Version fell back to %q despite having a %s", Fallback, name)
			}
		})
	}

	t.Run("not linker stamp", func(t *testing.T) {
		if got := resolve("v0.1.4", nil, false); got.Version != "v0.1.4" {
			t.Errorf("Version = %q, want v0.1.4 with no build info present", got.Version)
		}
	})
}

// A modified tree is a fact about the artifact, so it is flagged whichever
// source supplied the version.
func TestModifiedTreeIsFlagged(t *testing.T) {
	tests := map[string]struct {
		linker string
		module string
		want   string
	}{
		"linker stamp":   {linker: "v0.1.4", module: "v0.1.3", want: "v0.1.4+dirty"},
		"module version": {module: "v0.1.3", want: "v0.1.3+dirty"},
		"revision only":  {module: devel, want: shortRev + "+dirty"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := resolve(tt.linker, stamped(tt.module, fullRevision, commitTime, true), true)
			if got.Version != tt.want {
				t.Errorf("Version = %q, want %q", got.Version, tt.want)
			}
			if !got.Modified {
				t.Error("Modified = false on a modified tree")
			}
		})
	}
}

// The module system already appends +dirty to Main.Version, so a local build
// of a tagged but modified tree reported v0.1.3+dirty+dirty until this case
// was handled. Synthetic BuildInfo hid it; a real binary did not.
func TestDirtyMarkerIsNotDoubled(t *testing.T) {
	got := resolve("", stamped("v0.1.3"+dirtySuffix, fullRevision, commitTime, true), true)

	if got.Version != "v0.1.3+dirty" {
		t.Errorf("Version = %q, want a single dirty marker", got.Version)
	}
	if strings.Count(got.Version, dirtySuffix) != 1 {
		t.Errorf("Version = %q carries %d dirty markers", got.Version, strings.Count(got.Version, dirtySuffix))
	}
}

func TestCleanTreeIsNotFlagged(t *testing.T) {
	got := resolve("v0.1.4", stamped("v0.1.3", fullRevision, commitTime, false), true)

	if strings.Contains(got.Version, "dirty") {
		t.Errorf("Version = %q, want no dirty marker on a clean tree", got.Version)
	}
}

func TestStringCarriesWhatAReaderNeeds(t *testing.T) {
	out := resolve("v0.1.4", stamped("v0.1.3", fullRevision, commitTime, true), true).String()

	for _, want := range []string{"pix-sandbox", "v0.1.4+dirty", shortRev, commitTime, "go1.26.5", platform()} {
		if !strings.Contains(out, want) {
			t.Errorf("--version output is missing %q:\n%s", want, out)
		}
	}
	// The full hash is noise once the short one is there.
	if strings.Contains(out, fullRevision) {
		t.Errorf("--version output carries the full revision:\n%s", out)
	}
}

// Resolve is the exported path; it must agree with the cascade on the one
// input a test can control.
func TestResolveUsesTheLinkerStamp(t *testing.T) {
	if got := Resolve("v9.9.9"); got.Version != "v9.9.9" {
		t.Errorf("Resolve(%q).Version = %q", "v9.9.9", got.Version)
	}
	// A test binary is built from a module, so this must never be the
	// fallback — if it is, the cascade stopped reading build info.
	if got := Resolve(""); got.Version == "" {
		t.Error("Resolve(\"\") produced an empty version")
	}
}

func platform() string {
	return resolve("", nil, false).Platform
}
