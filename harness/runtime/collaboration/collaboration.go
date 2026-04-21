package collaboration

import (
	"fmt"
	"sort"
	"strings"
)

type Mode string

const (
	ModeExecute     Mode = "execute"
	ModePlan        Mode = "plan"
	ModeInvestigate Mode = "investigate"
)

type Capability string

const (
	CapabilityReadWorkspace              Capability = "read_workspace"
	CapabilityMutateWorkspace            Capability = "mutate_workspace"
	CapabilityExecuteCommands            Capability = "execute_commands"
	CapabilityAccessNetwork              Capability = "access_network"
	CapabilityCreateAsyncTasks           Capability = "create_async_tasks"
	CapabilityLoadTrustedWorkspaceAssets Capability = "load_trusted_workspace_assets"
	CapabilityWriteMemory                Capability = "write_memory"
	CapabilityMutateGraph                Capability = "mutate_graph"
)

type CapabilitySet map[Capability]struct{}

func NormalizeMode(raw string) Mode {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case string(ModePlan):
		return ModePlan
	case string(ModeInvestigate):
		return ModeInvestigate
	case string(ModeExecute):
		return ModeExecute
	default:
		return Mode(raw)
	}
}

func (m Mode) Validate() error {
	switch strings.ToLower(strings.TrimSpace(string(m))) {
	case string(ModeExecute), string(ModePlan), string(ModeInvestigate):
		return nil
	default:
		return fmt.Errorf("unknown collaboration mode %q", strings.TrimSpace(string(m)))
	}
}

func NewCapabilitySet(items ...Capability) CapabilitySet {
	set := make(CapabilitySet, len(items))
	for _, item := range items {
		item = Capability(strings.TrimSpace(string(item)))
		if item == "" {
			continue
		}
		set[item] = struct{}{}
	}
	return set
}

func AllCapabilities() CapabilitySet {
	return NewCapabilitySet(
		CapabilityReadWorkspace,
		CapabilityMutateWorkspace,
		CapabilityExecuteCommands,
		CapabilityAccessNetwork,
		CapabilityCreateAsyncTasks,
		CapabilityLoadTrustedWorkspaceAssets,
		CapabilityWriteMemory,
		CapabilityMutateGraph,
	)
}

func CeilingForMode(mode Mode) CapabilitySet {
	set := AllCapabilities()
	switch NormalizeMode(string(mode)) {
	case ModePlan:
		delete(set, CapabilityMutateWorkspace)
	case ModeInvestigate, ModeExecute:
	}
	return set
}

func IntersectSets(sets ...CapabilitySet) CapabilitySet {
	if len(sets) == 0 {
		return CapabilitySet{}
	}
	result := sets[0].Clone()
	for _, next := range sets[1:] {
		for capability := range result {
			if !next.Has(capability) {
				delete(result, capability)
			}
		}
	}
	return result
}

func (s CapabilitySet) Clone() CapabilitySet {
	if len(s) == 0 {
		return CapabilitySet{}
	}
	cloned := make(CapabilitySet, len(s))
	for capability := range s {
		cloned[capability] = struct{}{}
	}
	return cloned
}

func (s CapabilitySet) Has(capability Capability) bool {
	_, ok := s[capability]
	return ok
}

func (s CapabilitySet) Slice() []Capability {
	items := make([]Capability, 0, len(s))
	for capability := range s {
		items = append(items, capability)
	}
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	return items
}
