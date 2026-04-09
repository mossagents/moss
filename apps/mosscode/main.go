package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/logging"
)

func main() {
	if err := appkit.InitializeApp(appName, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	logging.EnableDebugFromArgs(os.Args[1:])
	_, _, debugCloser, err := logging.ConfigureDebugFileWhenEnabled(appconfig.AppDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: configure debug logging: %v\n", err)
		os.Exit(1)
	}
	if logging.DebugEnabled() {
		logging.GetLogger().Info("debug file logging enabled", "path", appconfig.AppDir()+"\\debug.log")
	}
	if debugCloser != nil {
		defer debugCloser.Close()
	}

	cfg := newConfig()
	root := buildRootCommand(cfg)
	exitOnCommandError(root.Execute())
}

func exitOnCommandError(err error) {
	if err == nil {
		return
	}
	var exitErr *commandExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.code)
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
