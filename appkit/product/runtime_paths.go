package product

import (
	"encoding/json"
	appconfig "github.com/mossagents/moss/config"
	"os"
	"path/filepath"
	"strings"
)

const disableStateCatalogEnv = "MOSSCODE_DISABLE_STATE_CATALOG"

func SessionStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "sessions")
}

func StateStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "state")
}

func StateEventDir() string {
	return filepath.Join(StateStoreDir(), "events")
}

func StateCatalogEnabled() bool {
	value := strings.TrimSpace(os.Getenv(disableStateCatalogEnv))
	if value == "" {
		return true
	}
	value = strings.ToLower(value)
	return value != "1" && value != "true" && value != "yes" && value != "on"
}

func CheckpointStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "checkpoints")
}

func ChangeStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "changes")
}

func MemoryDir() string {
	return filepath.Join(appconfig.AppDir(), "memories")
}

func TaskRuntimeDir() string {
	return filepath.Join(appconfig.AppDir(), "tasks")
}

func WorkspaceIsolationDir() string {
	return filepath.Join(appconfig.AppDir(), "workspaces")
}

func defaultPricingCatalogPath(workspace, explicit string) string {
	path := ResolvePricingCatalogPath(workspace, explicit)
	if path != "" {
		return path
	}
	candidates := pricingCatalogCandidates(workspace)
	if len(candidates) == 0 {
		return filepath.Join(appconfig.AppDir(), "pricing.yaml")
	}
	return candidates[0]
}

func detectedEnvVars() []string {
	keys := []string{
		"MOSSCODE_API_TYPE", "MOSSCODE_PROVIDER", "MOSSCODE_NAME", "MOSSCODE_MODEL", "MOSSCODE_WORKSPACE", "MOSSCODE_TRUST", "MOSSCODE_API_KEY", "MOSSCODE_BASE_URL",
		"MOSS_API_TYPE", "MOSS_PROVIDER", "MOSS_NAME", "MOSS_MODEL", "MOSS_WORKSPACE", "MOSS_TRUST", "MOSS_API_KEY", "MOSS_BASE_URL",
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := os.LookupEnv(key); ok {
			out = append(out, key)
		}
	}
	return out
}

func checkWritableDir(path string) PathStatus {
	status := PathStatus{Path: path}
	if strings.TrimSpace(path) == "" {
		status.Error = "path is empty"
		return status
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	f, err := os.CreateTemp(path, "doctor-*")
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Writable = true
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return status
}

func checkWritableFile(path string) PathStatus {
	status := PathStatus{Path: path}
	if strings.TrimSpace(path) == "" {
		status.Error = "path is empty"
		return status
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = pathExists(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Exists = true
	status.Writable = true
	_ = f.Close()
	return status
}

func pathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func MarshalJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
