// Package platform resolves the per-user locations atlas-ai owns.
//
// Resolution is a pure function of the target OS name and an environment
// lookup, so both supported platforms can be tested on any host. Paths are
// built with the target OS separator, never the host's.
package platform

import (
	"fmt"
	"path"
	"strings"
)

const appDir = "atlas-ai"

// Paths holds the resolved per-user locations.
type Paths struct {
	// Home is the user's home directory.
	Home string
	// StateDir is where atlas-ai keeps its own state.
	StateDir string
}

// Resolve returns the paths for goos using getenv for environment lookups.
// Empty or relative required values are rejected; there is no fallback to
// the current directory.
func Resolve(goos string, getenv func(string) string) (Paths, error) {
	switch goos {
	case "linux":
		return resolveLinux(getenv)
	case "windows":
		return resolveWindows(getenv)
	default:
		return Paths{}, fmt.Errorf("unsupported platform %q", goos)
	}
}

func resolveLinux(getenv func(string) string) (Paths, error) {
	home := getenv("HOME")
	if !isAbsPosix(home) {
		return Paths{}, fmt.Errorf("HOME must be set to an absolute path, got %q", home)
	}
	home = path.Clean(home)

	if xdg := getenv("XDG_STATE_HOME"); isAbsPosix(xdg) {
		return Paths{Home: home, StateDir: path.Join(xdg, appDir)}, nil
	}
	return Paths{Home: home, StateDir: path.Join(home, ".local", "state", appDir)}, nil
}

func resolveWindows(getenv func(string) string) (Paths, error) {
	home := getenv("USERPROFILE")
	if !isAbsWindows(home) {
		return Paths{}, fmt.Errorf("USERPROFILE must be set to an absolute path, got %q", home)
	}
	local := getenv("LOCALAPPDATA")
	if !isAbsWindows(local) {
		return Paths{}, fmt.Errorf("LOCALAPPDATA must be set to an absolute path, got %q", local)
	}
	return Paths{
		Home:     trimWindows(home),
		StateDir: joinWindows(local, appDir),
	}, nil
}

func isAbsPosix(p string) bool {
	return strings.HasPrefix(p, "/")
}

// isAbsWindows reports whether p is a drive-absolute path (C:\ or C:/) or a
// UNC path (\\server\share).
func isAbsWindows(p string) bool {
	if strings.HasPrefix(p, `\\`) {
		return len(p) > 2
	}
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// trimWindows removes trailing separators without touching a bare drive root
// such as C:\.
func trimWindows(p string) string {
	for len(p) > 3 && (strings.HasSuffix(p, `\`) || strings.HasSuffix(p, "/")) {
		p = p[:len(p)-1]
	}
	return p
}

// joinWindows appends elem to base with a backslash, avoiding a doubled
// separator when base is a drive root such as C:\.
func joinWindows(base, elem string) string {
	base = trimWindows(base)
	if strings.HasSuffix(base, `\`) || strings.HasSuffix(base, "/") {
		return base + elem
	}
	return base + `\` + elem
}
