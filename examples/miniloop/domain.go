package main

import (
	"context"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
)

// Domain defines a pluggable domain for the stateful loop agent.
//
// miniloop is a generic loop-based agent that delegates all domain-specific
// logic to a Domain adapter. Each domain provides its own tools, system
// prompt, policies, background processes, and UI hints.
//
// To add a new domain, implement this interface and call registerDomain
// in an init() function.
type Domain interface {
	// Name returns the domain identifier (e.g., "trading").
	Name() string

	// Description returns a short human-readable description.
	Description() string

	// Setup registers domain-specific tools on the kernel.
	Setup(k *kernel.Kernel) error

	// SystemPrompt returns the rendered system prompt for this domain.
	SystemPrompt(workspace string) string

	// Policies returns policy rules to apply on tool execution.
	Policies() []builtins.PolicyRule

	// EventHooks returns event-pattern → handler pairs.
	EventHooks() map[string]builtins.EventHandler

	// Start begins background processes (e.g., periodic state updates).
	// Returns a stop function. Called after kernel.Boot.
	Start(ctx context.Context) func()

	// Banner returns extra startup info lines to display.
	Banner() []string

	// Prompt returns the REPL prompt string (e.g., "💰 > ").
	Prompt() string
}

// domains is the registry of available domain adapters.
var domains = map[string]func(cfg *config) Domain{}

// registerDomain registers a domain adapter factory.
func registerDomain(name string, factory func(cfg *config) Domain) {
	domains[name] = factory
}
