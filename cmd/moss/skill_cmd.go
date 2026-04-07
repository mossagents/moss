package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/mossagents/moss/skill/registry"
)

const defaultRegistryURL = "https://registry.mossagents.io"

// skillCmd handles the `moss skill` subcommand.
func skillCmd(args []string) {
	if len(args) == 0 {
		printSkillUsage()
		os.Exit(0)
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list":
		skillListCmd(rest)
	case "search":
		skillSearchCmd(rest)
	case "install":
		skillInstallCmd(rest)
	case "remove", "uninstall":
		skillRemoveCmd(rest)
	case "info":
		skillInfoCmd(rest)
	case "help", "--help", "-h":
		printSkillUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown skill subcommand: %q\n", sub)
		printSkillUsage()
		os.Exit(1)
	}
}

func skillListCmd(args []string) {
	jsonOut := hasFlag(args, "--json")

	// Locally installed skills
	cache, err := registry.NewLocalCache("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	installed, err := cache.ListInstalled()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		printJSONValue(installed)
		return
	}

	if len(installed) == 0 {
		fmt.Println("No skills installed. Use `moss skill install <name>` to install one.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tINSTALLED AT\tDIR")
	for _, r := range installed {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.Version, r.InstalledAt.Format("2006-01-02"), r.Dir)
	}
	_ = w.Flush()
}

func skillSearchCmd(args []string) {
	jsonOut := hasFlag(args, "--json")
	query := strings.Join(filterFlags(args), " ")
	registryURL := envOr("MOSS_REGISTRY_URL", defaultRegistryURL)

	client := registry.NewHTTPRegistryClient(registryURL)
	ctx := context.Background()

	var entries []registry.RegistryEntry
	var err error
	if query == "" {
		entries, err = client.List(ctx)
	} else {
		entries, err = client.Search(ctx, registry.SearchOptions{Query: query, Limit: 50})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error searching registry: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		printJSONValue(entries)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No skills found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tAUTHOR\tDESCRIPTION")
	for _, e := range entries {
		desc := e.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, e.Version, e.Author, desc)
	}
	_ = w.Flush()
}

func skillInstallCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: moss skill install <name>[@version]")
		os.Exit(1)
	}
	nameVersion := filterFlags(args)
	if len(nameVersion) == 0 {
		fmt.Fprintln(os.Stderr, "usage: moss skill install <name>[@version]")
		os.Exit(1)
	}

	name, version := splitNameVersion(nameVersion[0])
	registryURL := envOr("MOSS_REGISTRY_URL", defaultRegistryURL)
	client := registry.NewHTTPRegistryClient(registryURL)
	ctx := context.Background()

	cache, err := registry.NewLocalCache("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Check if already installed
	if ok, _ := cache.IsInstalled(name, version); ok {
		v := version
		if v == "" {
			v = "latest"
		}
		fmt.Printf("Skill %q (%s) is already installed.\n", name, v)
		return
	}

	fmt.Printf("Looking up %q in registry...\n", name)
	entry, err := client.Get(ctx, name, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Installing %s@%s...\n", entry.Name, entry.Version)
	rec, err := cache.Install(ctx, *entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error installing: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Installed %s@%s to %s\n", rec.Name, rec.Version, rec.Dir)
}

func skillRemoveCmd(args []string) {
	nameVersion := filterFlags(args)
	if len(nameVersion) == 0 {
		fmt.Fprintln(os.Stderr, "usage: moss skill remove <name>[@version]")
		os.Exit(1)
	}

	name, version := splitNameVersion(nameVersion[0])
	cache, err := registry.NewLocalCache("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := cache.Remove(name, version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if version != "" {
		fmt.Printf("✓ Removed %s@%s\n", name, version)
	} else {
		fmt.Printf("✓ Removed all versions of %s\n", name)
	}
}

func skillInfoCmd(args []string) {
	nameVersion := filterFlags(args)
	if len(nameVersion) == 0 {
		fmt.Fprintln(os.Stderr, "usage: moss skill info <name>[@version]")
		os.Exit(1)
	}

	name, version := splitNameVersion(nameVersion[0])
	jsonOut := hasFlag(args, "--json")
	registryURL := envOr("MOSS_REGISTRY_URL", defaultRegistryURL)
	client := registry.NewHTTPRegistryClient(registryURL)
	ctx := context.Background()

	entry, err := client.Get(ctx, name, version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if jsonOut {
		printJSONValue(entry)
		return
	}

	fmt.Printf("Name:        %s\n", entry.Name)
	fmt.Printf("Version:     %s\n", entry.Version)
	fmt.Printf("Author:      %s\n", entry.Author)
	fmt.Printf("License:     %s\n", entry.License)
	fmt.Printf("Description: %s\n", entry.Description)
	if len(entry.Tools) > 0 {
		fmt.Printf("Tools:       %s\n", strings.Join(entry.Tools, ", "))
	}
	if len(entry.Requires) > 0 {
		fmt.Printf("Requires:    ")
		parts := make([]string, 0, len(entry.Requires))
		for _, r := range entry.Requires {
			s := r.Name
			if r.MinVersion != "" || r.MaxVersion != "" {
				s += fmt.Sprintf(" (%s..%s)", r.MinVersion, r.MaxVersion)
			}
			parts = append(parts, s)
		}
		fmt.Println(strings.Join(parts, ", "))
	}
	fmt.Printf("Downloads:   %d\n", entry.Downloads)
}

func printSkillUsage() {
	fmt.Print(`Usage: moss skill <subcommand> [options]

Subcommands:
  list              List locally installed skills
  search [query]    Search the skill registry
  install <name>    Install a skill from the registry (name[@version])
  remove  <name>    Remove an installed skill (name[@version])
  info    <name>    Show info about a skill from the registry

Options:
  --json            Output as JSON
  --registry <url>  Use a custom registry URL (default: MOSS_REGISTRY_URL env or https://registry.mossagents.io)
`)
}

// ---- helpers -------------------------------------------------------------

func splitNameVersion(s string) (name, version string) {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func filterFlags(args []string) []string {
	out := args[:0]
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			out = append(out, a)
		}
	}
	return out
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printJSONValue(v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
