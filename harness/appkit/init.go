package appkit

import (
	config "github.com/mossagents/moss/harness/config"
)

// InitializeApp sets app identity, ensures app dir exists, then resolves common flags.
func InitializeApp(name string, flags *AppFlags, envPrefixes ...string) error {
	config.SetAppName(name)
	if err := config.EnsureAppDir(); err != nil {
		return err
	}
	if flags != nil {
		flags.MergeGlobalConfig()
		flags.MergeEnv(envPrefixes...)
		flags.ApplyDefaults()
	}
	return nil
}
