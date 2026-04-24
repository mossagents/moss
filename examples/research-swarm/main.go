package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := runMain(os.Args[1:]); err != nil {
		var exitErr *commandExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintln(os.Stderr, exitErr.Error())
			os.Exit(exitErr.code)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runMain(args []string) error {
	cmd, err := parseCommand(args)
	if err != nil {
		return err
	}
	switch cmd.name {
	case commandRun:
		return runRunCommand(cmd.run)
	case commandResume:
		return runResumeCommand(cmd.resume)
	case commandInspect:
		return runInspectCommand(cmd.inspect)
	case commandExport:
		return runExportCommand(cmd.export)
	default:
		return usageError(2, rootUsage())
	}
}
