package main

import (
	"errors"
	"fmt"
	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/logging"
	"os"
)

func main() {
	if err := appkit.InitializeApp(appName, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	_, _, debugCloser, err := logging.ConfigureDebugFileWhenEnabled(appconfig.AppDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: configure debug logging: %v\n", err)
		os.Exit(1)
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
