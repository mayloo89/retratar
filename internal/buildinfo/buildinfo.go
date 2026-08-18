// Package buildinfo reports what this binary was built from.
//
// The values come from [runtime/debug.ReadBuildInfo], which the Go toolchain
// stamps into every binary built inside a version control checkout. No
// -ldflags -X wiring is needed, and no Makefile can forget to pass it.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"sync"
)

// Info describes the built binary.
type Info struct {
	Version  string // module version, or "devel" outside a release
	Revision string // VCS commit, or "unknown"
	Time     string // commit timestamp, RFC 3339
	Dirty    bool   // built from a tree with uncommitted changes
	GoVer    string
}

var read = sync.OnceValue(func() Info {
	info := Info{Version: "devel", Revision: "unknown", GoVer: runtime.Version()}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if bi.Main.Version != "" {
		info.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.time":
			info.Time = s.Value
		case "vcs.modified":
			info.Dirty = s.Value == "true"
		}
	}
	return info
})

// Get returns the build information. It is computed once.
func Get() Info { return read() }

// String renders the build for a log line.
func (i Info) String() string {
	s := i.Version + " (" + i.short() + ")"
	if i.Dirty {
		s += " [dirty]"
	}
	return s + " " + i.GoVer
}

func (i Info) short() string {
	const n = 12
	if len(i.Revision) > n {
		return i.Revision[:n]
	}
	return i.Revision
}
