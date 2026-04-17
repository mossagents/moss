package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/harness/appkit/product"
	configpkg "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	rprofile "github.com/mossagents/moss/harness/runtime/profile"
	"github.com/mossagents/moss/harness/userio/prompting"
	"strings"
)

// initKernelCmd 异步创建 kernel + session。
func initKernelCmd(cfg Config, wCfg WelcomeConfig, bridge *bridgeIO) tea.Cmd {
	return func() tea.Msg {
		state, err := newKernelInitState(cfg, wCfg, bridge)
		if err != nil {
			return sessionResultMsg{err: err}
		}
		if err := state.initSession(); err != nil {
			return sessionResultMsg{err: err}
		}
		agent := state.buildAgent()
		state.k.InstallPlugin(agent.permissionOverridePlugin())
		return kernelReadyMsg{agent: agent, notices: state.notices}
	}
}

type kernelInitState struct {
	cfg      Config
	wCfg     WelcomeConfig
	bridge   *bridgeIO
	provider string

	k      *kernel.Kernel
	ctx    context.Context
	cancel context.CancelFunc

	store   session.SessionStore
	sess    *session.Session
	notices []string
}

func newKernelInitState(cfg Config, wCfg WelcomeConfig, bridge *bridgeIO) (*kernelInitState, error) {
	provider := strings.ToLower(configpkg.NormalizeProviderIdentity(wCfg.Provider, wCfg.ProviderName).EffectiveAPIType())
	k, ctx, cancel, err := buildRuntimeKernel(cfg, wCfg, bridge)
	if err != nil {
		return nil, err
	}
	state := &kernelInitState{
		cfg:      cfg,
		wCfg:     wCfg,
		bridge:   bridge,
		provider: provider,
		k:        k,
		ctx:      ctx,
		cancel:   cancel,
	}
	if strings.TrimSpace(cfg.SessionStoreDir) != "" {
		state.store, _ = session.NewFileStore(cfg.SessionStoreDir)
	}
	return state, nil
}

func (s *kernelInitState) initSession() error {
	if strings.TrimSpace(s.cfg.InitialSessionID) != "" {
		return s.loadInitialSession()
	}
	return s.createInteractiveSession()
}

func (s *kernelInitState) loadInitialSession() error {
	if s.store == nil {
		s.cancel()
		return fmt.Errorf("failed to load session %q: session store is unavailable", s.cfg.InitialSessionID)
	}
	sess, err := s.store.Load(s.ctx, s.cfg.InitialSessionID)
	if err != nil {
		s.cancel()
		return fmt.Errorf("failed to load session %q: %w", s.cfg.InitialSessionID, err)
	}
	if sess == nil {
		s.cancel()
		return fmt.Errorf("session %q not found", s.cfg.InitialSessionID)
	}
	s.sess = sess
	currentPosture, err := postureFromRuntime(s.wCfg.Workspace, s.cfg.Profile, s.cfg.Trust, s.cfg.ApprovalMode)
	if err != nil {
		s.cancel()
		return err
	}
	plan, err := planPostureRebuild(
		s.cfg.InitialSessionID,
		currentPosture,
		rprofile.SessionPostureFromSession(s.sess),
	)
	if err != nil {
		s.cancel()
		return err
	}
	if err := s.applyPostureRebuild(plan); err != nil {
		return err
	}
	if strings.TrimSpace(plan.Notice) != "" {
		s.notices = append(s.notices, plan.Notice)
	}
	return nil
}

func (s *kernelInitState) applyPostureRebuild(plan postureRebuildPlan) error {
	if !plan.Rebuild {
		return nil
	}
	s.cancel()
	rebuildProfile := strings.TrimSpace(s.cfg.Profile)
	if rebuildProfile == "" {
		rebuildProfile = "default"
	}
	k, ctx, cancel, err := buildRuntimeKernel(Config{
		Trust:        plan.Resolved.Trust,
		Profile:      rebuildProfile,
		ApprovalMode: plan.Resolved.ApprovalMode,
		APIKey:       s.cfg.APIKey,
		BaseURL:      s.cfg.BaseURL,
		BuildKernel:  s.cfg.BuildKernel,
		AfterBoot:    s.cfg.AfterBoot,
	}, s.wCfg, s.bridge)
	if err != nil {
		return err
	}
	s.k, s.ctx, s.cancel = k, ctx, cancel
	if err := product.ApplyResolvedProfile(s.k, plan.Resolved); err != nil {
		s.cancel()
		return fmt.Errorf("apply rebuilt posture: %w", err)
	}
	s.cfg.Trust = plan.Resolved.Trust
	s.cfg.Profile = strings.TrimSpace(plan.Resolved.Name)
	s.cfg.ApprovalMode = plan.Resolved.ApprovalMode
	return nil
}

func (s *kernelInitState) createInteractiveSession() error {
	sessCfg := normalizeSessionConfigDefaults(session.SessionConfig{
		Goal:       "interactive",
		Mode:       "interactive",
		TrustLevel: s.cfg.Trust,
		Profile:    s.cfg.Profile,
		MaxSteps:   200,
	}, s.cfg.Trust, s.cfg.Profile, "interactive", "interactive", 200)
	if s.cfg.BuildSessionConfig != nil {
		sessCfg = normalizeSessionConfigDefaults(
			s.cfg.BuildSessionConfig(s.wCfg.Workspace, s.cfg.Trust, s.cfg.ApprovalMode, s.cfg.Profile, ""),
			s.cfg.Trust,
			s.cfg.Profile,
			"interactive",
			"interactive",
			200,
		)
	}
	metadata := preparePromptMetadata(sessCfg, s.cfg.Profile)
	sysPrompt, err := prompting.ComposeSystemPrompt(
		s.wCfg.Workspace,
		s.cfg.Trust,
		s.k,
		s.cfg.PromptConfigInstructions,
		s.cfg.PromptModelInstructions,
		metadata,
	)
	if err != nil {
		s.cancel()
		return fmt.Errorf("failed to compose system prompt: %w", err)
	}
	sessCfg.SystemPrompt = sysPrompt
	sessCfg.Metadata = metadata
	s.sess, err = s.k.NewSession(s.ctx, sessCfg)
	if err != nil {
		s.cancel()
		return fmt.Errorf("failed to create session: %w", err)
	}
	return nil
}

func (s *kernelInitState) buildAgent() *agentState {
	return &agentState{
		k:                        s.k,
		sess:                     s.sess,
		store:                    s.store,
		ctx:                      s.ctx,
		cancel:                   s.cancel,
		bridge:                   s.bridge,
		workspace:                s.wCfg.Workspace,
		trust:                    s.cfg.Trust,
		profile:                  s.cfg.Profile,
		approvalMode:             s.cfg.ApprovalMode,
		baseObserver:             s.cfg.BaseObserver,
		buildRunTraceObserver:    s.cfg.BuildRunTraceObserver,
		buildKernel:              s.cfg.BuildKernel,
		afterBoot:                s.cfg.AfterBoot,
		buildSessionConfig:       s.cfg.BuildSessionConfig,
		promptConfigInstructions: s.cfg.PromptConfigInstructions,
		promptModelInstructions:  s.cfg.PromptModelInstructions,
		provider:                 s.provider,
		model:                    s.wCfg.Model,
		apiKey:                   s.cfg.APIKey,
		baseURL:                  s.cfg.BaseURL,
		permissions:              map[string]string{},
	}
}
