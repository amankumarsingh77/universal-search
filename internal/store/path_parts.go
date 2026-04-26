// Package store provides SQLite-backed persistence for file and chunk metadata.
package store

import (
	"path/filepath"
	"strings"
)

// PathParts decomposes a file path into its three derived components used for
// filename search indexing.
//
//   - basename: the final path element (e.g. "report.pdf" from "/docs/report.pdf").
//   - parent:   the directory portion without a trailing slash
//               (e.g. "/docs" from "/docs/report.pdf"; "" for a bare name).
//   - stem:     basename minus its last dot-extension
//               (e.g. "archive.tar" from "archive.tar.gz"; unchanged if no dot).
//
// Trailing slashes in path are stripped before processing (filepath.Clean
// normalises these). A root path ("/") returns three empty strings.
func PathParts(path string) (basename, parent, stem string) {
	if path == "" {
		return "", "", ""
	}

	// Clean removes trailing slashes and resolves dots.
	cleaned := filepath.Clean(path)

	// filepath.Base returns "." for an empty result; guard against that.
	basename = filepath.Base(cleaned)
	if basename == "." || basename == "/" {
		return "", "", ""
	}

	// filepath.Dir returns "." when there is no directory component.
	d := filepath.Dir(cleaned)
	if d == "." {
		parent = ""
	} else {
		parent = d
	}

	// Stem: basename minus its last dot-extension.
	// strings.LastIndex finds the last '.'. A leading dot on a hidden file
	// (e.g. ".bashrc") is not treated as an extension separator — only a dot
	// that is not the very first character is considered.
	dotIdx := strings.LastIndex(basename, ".")
	if dotIdx <= 0 {
		// No dot, or only a leading dot (hidden file with no extension).
		stem = basename
	} else {
		stem = basename[:dotIdx]
	}

	return basename, parent, stem
}
