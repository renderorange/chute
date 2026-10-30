package config

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultDownloadDir is where attachments go when download_dir is unset.
const defaultDownloadDir = "~/Downloads"

// DownloadPath is the directory attachments are saved into: the configured
// download_dir with a leading ~ expanded, or ~/Downloads when it is empty.
func (g General) DownloadPath() string {
	if g.DownloadDir == "" {
		return ExpandPath(defaultDownloadDir)
	}

	return ExpandPath(g.DownloadDir)
}

// ExpandPath expands a leading ~ or ~/ to the user's home directory. Other
// paths are returned unchanged.
func ExpandPath(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}

	return filepath.Join(home, path[2:])
}
