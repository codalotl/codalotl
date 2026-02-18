package cascade

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var isWindows = runtime.GOOS == "windows" // isWindows reports whether the current OS is Windows.

// ExpandPath expands out leading ~ (meaning home directory) to an absolute path. Works cross-OS (including Windows, which doesn't traditionally treat ~ as the home
// directory).
func ExpandPath(path string) string {
	if path == "" {
		return ""
	}

	expanded := path

	// Expand a leading "~", "~/" or "~\" to the user's home directory:
	if strings.HasPrefix(expanded, "~") {
		if home, _ := os.UserHomeDir(); home != "" {
			switch {
			case expanded == "~" || expanded == "~/" || expanded == `~\`:
				expanded = home
			case strings.HasPrefix(expanded, "~/") || strings.HasPrefix(expanded, `~\`):
				expanded = filepath.Join(home, expanded[2:])
			}
		}
	}

	if !filepath.IsAbs(expanded) {
		if abs, err := filepath.Abs(expanded); err == nil {
			expanded = abs
		}
	}

	return expanded
}

// InUserConfigDirectory returns an absolute path to a good location to write user-specific config files, joined with subPath. Ex:
//   - InUserConfigDirectory("foo/foo.json") -> "~/foo/foo.json" on OSX or Linux (but where ~ is expanded).
//   - InUserConfigDirectory("foo/foo.json") -> "%USERPROFILE%/AppData/Local/foo/foo.json" on Windows (but where %USERPROFILE% is expanded.
func InUserConfigDirectory(subPath string) string {
	if isWindows {
		return filepath.Join(ExpandPath("~/AppData/Local"), subPath)
	}
	return filepath.Join(ExpandPath("~"), subPath)
}
