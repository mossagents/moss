package sandbox

import (
	"os"
	"runtime"
	"strings"
)

var inheritedEnvAllowlist = []string{
	"HOME",
	"LANG",
	"LC_ALL",
	"PATH",
	"SHELL",
	"TERM",
	"TMP",
	"TEMP",
	"TZ",
	"USER",
	"USERPROFILE",
}

// SafeInheritedEnvironment returns the minimal inherited process environment
// required for local tools to resolve binaries and temp directories.
func SafeInheritedEnvironment() map[string]string {
	out := make(map[string]string)
	keys := append([]string(nil), inheritedEnvAllowlist...)
	if runtime.GOOS == "windows" {
		keys = append(keys, "APPDATA", "ComSpec", "LOCALAPPDATA", "PATHEXT", "ProgramData", "SystemDrive", "SystemRoot", "WINDIR")
	}
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			out[key] = value
		}
	}
	return out
}
