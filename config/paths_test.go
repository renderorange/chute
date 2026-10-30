package config

import (
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare tilde", "~", home},
		{"home prefix", "~/Downloads", filepath.Join(home, "Downloads")},
		{"absolute", "/srv/mail", "/srv/mail"},
		{"relative", "mail", "mail"},
		{"embedded tilde", "/srv/~/mail", "/srv/~/mail"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandPath(tc.in); got != tc.want {
				t.Errorf("ExpandPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDownloadPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		g    General
		want string
	}{
		{"default", General{}, filepath.Join(home, "Downloads")},
		{"configured", General{DownloadDir: "/srv/mail"}, "/srv/mail"},
		{"tilde", General{DownloadDir: "~/attachments"}, filepath.Join(home, "attachments")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.DownloadPath(); got != tc.want {
				t.Errorf("DownloadPath() = %q, want %q", got, tc.want)
			}
		})
	}
}
