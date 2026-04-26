package store

import (
	"testing"
)

// TestPathParts covers the PathParts helper for all documented edge cases.
func TestPathParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		wantBasename string
		wantParent   string
		wantStem     string
	}{
		{
			name:         "normal absolute path",
			path:         "/home/user/documents/report.pdf",
			wantBasename: "report.pdf",
			wantParent:   "/home/user/documents",
			wantStem:     "report",
		},
		{
			name:         "no extension",
			path:         "/usr/bin/ls",
			wantBasename: "ls",
			wantParent:   "/usr/bin",
			wantStem:     "ls",
		},
		{
			name:         "multi-dot name",
			path:         "/home/user/archive.tar.gz",
			wantBasename: "archive.tar.gz",
			wantParent:   "/home/user",
			wantStem:     "archive.tar",
		},
		{
			name:         "single-component name no slash",
			path:         "README",
			wantBasename: "README",
			wantParent:   "",
			wantStem:     "README",
		},
		{
			name:         "single-component name with extension",
			path:         "setup.py",
			wantBasename: "setup.py",
			wantParent:   "",
			wantStem:     "setup",
		},
		{
			name:         "trailing slash stripped",
			path:         "/home/user/",
			wantBasename: "user",
			wantParent:   "/home",
			wantStem:     "user",
		},
		{
			name:         "path with trailing slash and extension",
			path:         "/home/user/file.txt/",
			wantBasename: "file.txt",
			wantParent:   "/home/user",
			wantStem:     "file",
		},
		{
			name:         "root path",
			path:         "/",
			wantBasename: "",
			wantParent:   "",
			wantStem:     "",
		},
		{
			name:         "hidden file (dot-prefix, no extension)",
			path:         "/home/user/.bashrc",
			wantBasename: ".bashrc",
			wantParent:   "/home/user",
			wantStem:     ".bashrc",
		},
		{
			name:         "hidden file with extension",
			path:         "/home/user/.config.toml",
			wantBasename: ".config.toml",
			wantParent:   "/home/user",
			wantStem:     ".config",
		},
		{
			name:         "file directly under root",
			path:         "/etc",
			wantBasename: "etc",
			wantParent:   "/",
			wantStem:     "etc",
		},
		{
			name:         "deeply nested",
			path:         "/a/b/c/d/e.log",
			wantBasename: "e.log",
			wantParent:   "/a/b/c/d",
			wantStem:     "e",
		},
		{
			name:         "empty path",
			path:         "",
			wantBasename: "",
			wantParent:   "",
			wantStem:     "",
		},
		{
			name:         "windows-style path (backslash not treated as separator)",
			path:         `C:\Users\foo\bar.txt`,
			wantBasename: `C:\Users\foo\bar.txt`,
			wantParent:   "",
			wantStem:     `C:\Users\foo\bar`,
		},
		{
			name:         "multiple extensions foo.tar.gz deeper path",
			path:         "/opt/backups/data.tar.gz",
			wantBasename: "data.tar.gz",
			wantParent:   "/opt/backups",
			wantStem:     "data.tar",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotBasename, gotParent, gotStem := PathParts(tc.path)
			if gotBasename != tc.wantBasename {
				t.Errorf("PathParts(%q).basename = %q, want %q", tc.path, gotBasename, tc.wantBasename)
			}
			if gotParent != tc.wantParent {
				t.Errorf("PathParts(%q).parent = %q, want %q", tc.path, gotParent, tc.wantParent)
			}
			if gotStem != tc.wantStem {
				t.Errorf("PathParts(%q).stem = %q, want %q", tc.path, gotStem, tc.wantStem)
			}
		})
	}
}
