package appkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	rpolicy "github.com/mossagents/moss/runtime/policy"
	rprobe "github.com/mossagents/moss/runtime/probe"
	rstate "github.com/mossagents/moss/runtime/state"
)

type deepAgentPresetState struct {
	flags  *AppFlags
	config DeepAgentConfig
	appDir string

	stateCatalog     *rstate.StateCatalog
	isolationRoot    string
	isolationEnabled bool
}

type deepAgentPack struct {
	name  string
	build func(*deepAgentPresetState) ([]harness.Feature, error)
}

func buildDeepAgentFeatures(flags *AppFlags, cfg DeepAgentConfig) ([]harness.Feature, error) {
	state := &deepAgentPresetState{
		flags:  flags,
		config: cfg,
		appDir: appconfig.AppDir(),
	}
	packs := []deepAgentPack{
		{name: "additional-features", build: buildDeepAgentAdditionalFeaturesPack},
		{name: "state-catalog", build: buildDeepAgentStateCatalogPack},
		{name: "session-context", build: buildDeepAgentSessionContextPack},
		{name: "checkpoint-store", build: buildDeepAgentCheckpointPack},
		{name: "task-runtime", build: buildDeepAgentTaskRuntimePack},
		{name: "persistent-memories", build: buildDeepAgentPersistentMemoryPack},
		{name: "execution-services", build: buildDeepAgentExecutionPack},
		{name: "planning-profile", build: buildDeepAgentPlanningPack},
		{name: "bootstrap-context", build: buildDeepAgentBootstrapPack},
		{name: "llm-governance", build: buildDeepAgentLLMGovernancePack},
		{name: "runtime-setup", build: buildDeepAgentRuntimePack},
		{name: "post-runtime-governance", build: buildDeepAgentPostRuntimePack},
	}

	features := make([]harness.Feature, 0, len(cfg.AdditionalFeatures)+16)
	for _, pack := range packs {
		packFeatures, err := pack.build(state)
		if err != nil {
			return nil, fmt.Errorf("%s pack: %w", pack.name, err)
		}
		features = append(features, packFeatures...)
	}
	return features, nil
}

func buildDeepAgentAdditionalFeaturesPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if len(state.config.AdditionalFeatures) == 0 {
		return nil, nil
	}
	return append([]harness.Feature(nil), state.config.AdditionalFeatures...), nil
}

func buildDeepAgentStateCatalogPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if strings.TrimSpace(state.appDir) == "" {
		return nil, nil
	}
	stateDir := filepath.Join(state.appDir, "state")
	catalog, err := rstate.NewStateCatalog(stateDir, filepath.Join(stateDir, "events"), true)
	if err != nil {
		return nil, fmt.Errorf("state catalog: %w", err)
	}
	state.stateCatalog = catalog
	return []harness.Feature{
		harness.StateCatalog(catalog),
	}, nil
}

func buildDeepAgentSessionContextPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if !deepAgentValueOrDefault(state.config.EnableSessionStore, true) {
		return nil, nil
	}

	storeDir := state.config.SessionStoreDir
	if storeDir == "" {
		storeDir = state.defaultDataDir("sessions")
	}
	rawStore, err := session.NewFileStore(storeDir)
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}

	var store session.SessionStore = rstate.WrapSessionStore(rawStore, state.stateCatalog)
	features := []harness.Feature{harness.SessionPersistence(store)}
	if deepAgentValueOrDefault(state.config.EnableContextOffload, true) {
		features = append(features, harness.ContextOffload(store), harness.ContextManagement(store))
	}
	return features, nil
}

func buildDeepAgentCheckpointPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if !deepAgentValueOrDefault(state.config.EnableCheckpointStore, true) {
		return nil, nil
	}

	checkpointDir := state.config.CheckpointStoreDir
	if checkpointDir == "" {
		checkpointDir = state.defaultDataDir("checkpoints")
	}
	store, err := checkpoint.NewFileCheckpointStore(checkpointDir)
	if err != nil {
		return nil, fmt.Errorf("checkpoint store: %w", err)
	}
	return []harness.Feature{
		harness.Checkpointing(rstate.WrapCheckpointStore(store, state.stateCatalog)),
	}, nil
}

func buildDeepAgentTaskRuntimePack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if !deepAgentValueOrDefault(state.config.EnableTaskRuntime, true) {
		return nil, nil
	}

	taskDir := state.config.TaskRuntimeDir
	if taskDir == "" {
		taskDir = state.defaultDataDir("tasks")
	}
	taskRuntime, err := taskrt.NewFileTaskRuntime(taskDir)
	if err != nil {
		return nil, fmt.Errorf("task runtime: %w", err)
	}
	return []harness.Feature{
		harness.TaskDelegation(rstate.WrapTaskRuntime(taskRuntime, state.stateCatalog)),
	}, nil
}

func buildDeepAgentPersistentMemoryPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if !deepAgentValueOrDefault(state.config.EnablePersistentMemories, true) {
		return nil, nil
	}

	memDir := state.config.MemoryDir
	if memDir == "" {
		memDir = state.defaultDataDir("memories")
	}
	return []harness.Feature{
		harness.PersistentMemories(memDir),
	}, nil
}

func buildDeepAgentExecutionPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	state.isolationEnabled = deepAgentValueOrDefault(state.config.EnableWorkspaceIsolation, true)
	state.isolationRoot = state.config.IsolationRootDir
	if state.isolationRoot == "" {
		state.isolationRoot = state.defaultDataDir("workspaces")
	}
	if state.isolationEnabled {
		if err := os.MkdirAll(state.isolationRoot, 0o755); err != nil {
			return nil, fmt.Errorf("workspace isolation root: %w", err)
		}
	}

	probe := rprobe.NewExecutionProbe(state.flags.Workspace, state.isolationRoot, state.isolationEnabled)
	if err := probe.Error(rprobe.CapabilityExecutionIsolation); err != nil {
		return nil, fmt.Errorf("workspace isolation: %w", err)
	}
	return []harness.Feature{
		harness.ExecutionServices(state.flags.Workspace, state.isolationRoot, state.isolationEnabled),
	}, nil
}

func buildDeepAgentPlanningPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if !strings.EqualFold(strings.TrimSpace(state.flags.Profile), "planning") {
		return nil, nil
	}
	return []harness.Feature{
		harness.Planning(),
	}, nil
}

func buildDeepAgentBootstrapPack(state *deepAgentPresetState) ([]harness.Feature, error) {
	if !deepAgentValueOrDefault(state.config.EnableBootstrapContext, true) {
		return nil, nil
	}
	return []harness.Feature{
		harness.BootstrapContext(state.flags.Workspace, state.config.AppName, state.flags.Trust),
	}, nil
}

func buildDeepAgentLLMGovernancePack(state *deepAgentPresetState) ([]harness.Feature, error) {
	var retryCfg *retry.Config
	if deepAgentValueOrDefault(state.config.EnableDefaultLLMRetry, true) {
		retryCfg = state.config.LLMRetryConfig
		if retryCfg == nil {
			retryCfg = deepAgentDefaultRetryConfig()
		}
	}
	if retryCfg == nil && state.config.LLMBreakerConfig == nil {
		return nil, nil
	}
	return []harness.Feature{
		harness.LLMResilience(retryCfg, state.config.LLMBreakerConfig),
	}, nil
}

func buildDeepAgentRuntimePack(state *deepAgentPresetState) ([]harness.Feature, error) {
	return []harness.Feature{
		harness.RuntimeSetup(state.flags.Workspace, state.flags.Trust, state.config.DefaultSetupOptions...),
	}, nil
}

func buildDeepAgentPostRuntimePack(state *deepAgentPresetState) ([]harness.Feature, error) {
	features := []harness.Feature{
		harness.PatchToolCalls(),
	}
	if appconfig.NormalizeTrustLevel(state.flags.Trust) == appconfig.TrustRestricted &&
		deepAgentValueOrDefault(state.config.EnableDefaultRestrictedPolicy, true) {
		features = append(features, harness.ToolPolicy(
			rpolicy.ResolveToolPolicyForWorkspace(state.flags.Workspace, state.flags.Trust, "confirm"),
		))
	}
	features = append(features, harness.ExecutionCapabilityReport(state.flags.Workspace, state.isolationRoot, state.isolationEnabled))
	if deepAgentValueOrDefault(state.config.EnsureGeneralPurpose, true) {
		features = append(features, deepAgentGeneralPurposeFeature(state.flags, state.config))
	}
	return features, nil
}

func (s *deepAgentPresetState) defaultDataDir(name string) string {
	if strings.TrimSpace(s.appDir) != "" {
		return filepath.Join(s.appDir, name)
	}
	return filepath.Join(s.flags.Workspace, "."+s.config.AppName, name)
}

func deepAgentDefaultRetryConfig() *retry.Config {
	return &retry.Config{
		MaxRetries:   2,
		InitialDelay: 300 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		Multiplier:   2.0,
	}
}

func deepAgentGeneralPurposeFeature(flags *AppFlags, cfg DeepAgentConfig) harness.Feature {
	return harness.FeatureFunc{
		FeatureName: "general-purpose-agent",
		MetadataValue: harness.FeatureMetadata{
			Key:      "general-purpose-agent",
			Phase:    harness.FeaturePhasePostRuntime,
			Requires: []string{"runtime-setup"},
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			return ensureGeneralPurposeAgent(h.Kernel(), flags, cfg)
		},
	}
}

