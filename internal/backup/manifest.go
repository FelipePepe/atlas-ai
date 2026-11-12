// Package backup keeps timestamped backup sets of the files atlas-ai may
// modify, so every change can be undone.
//
// A set lives in <root>/<id>/ and holds a manifest.json plus a files/
// directory with one blob per file that existed when it was tracked.
package backup

import "regexp"

const (
	// ManifestVersion is the only manifest format version this package reads
	// and writes.
	ManifestVersion = 1

	// StatusOpen marks a set whose transaction has not been committed.
	StatusOpen = "open"
	// StatusComplete marks a committed set.
	StatusComplete = "complete"

	manifestName = "manifest.json"
	blobsDir     = "files"
)

// idPattern is the only accepted shape of a backup set id.
var idPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{4}$`)

// ValidID reports whether id has the exact shape of a backup set id. Every id
// read from the user or from disk must pass it before touching the filesystem.
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

// Manifest describes one backup set.
type Manifest struct {
	Version int     `json:"version"`
	ID      string  `json:"id"`
	Label   string  `json:"label"`
	Created string  `json:"created"` // RFC3339, UTC
	Status  string  `json:"status"`
	Entries []Entry `json:"entries"`
}

// Entry describes one tracked file.
type Entry struct {
	// Path is the absolute path of the tracked file.
	Path string `json:"path"`
	// Existed reports whether the file existed when it was tracked.
	Existed bool `json:"existed"`
	// Blob is the name of the copy under files/; empty when Existed is false.
	Blob string `json:"blob"`
	// Mode holds the permission bits of the file when it was tracked.
	Mode uint32 `json:"mode"`
	// BeforeSHA256 is the hex SHA-256 of the file when it was tracked.
	BeforeSHA256 string `json:"before_sha256"`
	// AfterSHA256 is the hex SHA-256 of the file at commit time.
	AfterSHA256 string `json:"after_sha256"`
	// AfterExists reports whether the file existed at commit time.
	AfterExists bool `json:"after_exists"`
}
