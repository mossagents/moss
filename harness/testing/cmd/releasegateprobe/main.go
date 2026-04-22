package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mossagents/moss/harness/testing/releasegate"
)

func main() {
	var env string
	var jsonOutput string
	flag.StringVar(&env, "env", "prod", "release gate environment: prod|staging|dev")
	flag.StringVar(&jsonOutput, "json-output", "", "optional path to write the probe report JSON")
	flag.Parse()

	report := releasegate.Run(context.Background(), env)
	if jsonOutput != "" {
		if err := writeJSON(jsonOutput, report); err != nil {
			fmt.Fprintf(os.Stderr, "write json report: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Print(releasegate.RenderReport(report))
	if !report.Passed() {
		os.Exit(1)
	}
}

func writeJSON(path string, report releasegate.ProbeReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
