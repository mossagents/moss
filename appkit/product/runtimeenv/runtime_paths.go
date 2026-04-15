package runtimeenv

import (
	appconfig "github.com/mossagents/moss/config"
	"path/filepath"
)

func SessionStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "sessions")
}

func StateStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "state")
}

func StateEventDir() string {
	return filepath.Join(StateStoreDir(), "events")
}

func CheckpointStoreDir() string {
	return filepath.Join(appconfig.AppDir(), "checkpoints")
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

