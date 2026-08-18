package buildinfo_test

import (
	"strings"
	"testing"

	"github.com/mayloo89/retratar/internal/buildinfo"
)

// TestGetIsPopulated checks that the toolchain stamped the binary. The values
// come from the VCS checkout, so a test binary built inside the repository has
// them; nothing here asserts a specific revision.
func TestGetIsPopulated(t *testing.T) {
	t.Parallel()

	info := buildinfo.Get()

	if info.Version == "" {
		t.Error("Version is empty")
	}
	if info.Revision == "" {
		t.Error("Revision is empty")
	}
	if !strings.HasPrefix(info.GoVer, "go") {
		t.Errorf("GoVer = %q, want a go... version", info.GoVer)
	}
}

func TestGetIsStable(t *testing.T) {
	t.Parallel()

	if buildinfo.Get() != buildinfo.Get() {
		t.Error("Get() returned different values across calls")
	}
}

func TestStringIncludesVersionAndGoVersion(t *testing.T) {
	t.Parallel()

	info := buildinfo.Get()
	s := info.String()

	if !strings.Contains(s, info.Version) {
		t.Errorf("String() = %q, want it to contain version %q", s, info.Version)
	}
	if !strings.Contains(s, info.GoVer) {
		t.Errorf("String() = %q, want it to contain Go version %q", s, info.GoVer)
	}
}

// TestStringTruncatesRevision keeps the startup log line readable: a full
// 40-character SHA in every line is noise.
func TestStringTruncatesRevision(t *testing.T) {
	t.Parallel()

	info := buildinfo.Get()
	if len(info.Revision) <= 12 {
		t.Skip("revision is already short")
	}
	if strings.Contains(info.String(), info.Revision) {
		t.Errorf("String() = %q contains the full revision, want it shortened", info.String())
	}
}
