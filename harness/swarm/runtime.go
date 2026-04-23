package swarm

import (
	"context"
	"fmt"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

const runtimeServiceKey kernel.ServiceKey = "harness.swarm.runtime"

// Runtime adapts kernel ports into a harness-owned swarm runtime object.
type Runtime struct {
	Kernel    *kernel.Kernel
	Sessions  session.SessionCatalog
	Tasks     taskrt.TaskRuntime
	Graph     taskrt.TaskGraphRuntime
	Messages  taskrt.TaskMessageRuntime
	Artifacts artifact.Store
	roles     map[kswarm.Role]RoleSpec
}

// NewRuntime assembles a runtime adapter from the configured kernel ports.
func NewRuntime(k *kernel.Kernel, specs ...RoleSpec) (*Runtime, error) {
	if k == nil {
		return nil, fmt.Errorf("kernel must not be nil")
	}
	store := k.SessionStore()
	if store == nil {
		return nil, fmt.Errorf("kernel session store is required")
	}
	tasks := k.TaskRuntime()
	if tasks == nil {
		return nil, fmt.Errorf("kernel task runtime is required")
	}
	graph, ok := tasks.(taskrt.TaskGraphRuntime)
	if !ok {
		return nil, fmt.Errorf("kernel task runtime must implement TaskGraphRuntime")
	}
	var messageRuntime taskrt.TaskMessageRuntime
	if queue, ok := tasks.(taskrt.TaskMessageRuntime); ok {
		messageRuntime = queue
	}

	roles := specs
	if len(roles) == 0 {
		roles = DefaultResearchRolePack()
	}
	roleMap := make(map[kswarm.Role]RoleSpec, len(roles))
	for _, spec := range roles {
		if err := spec.Validate(); err != nil {
			return nil, err
		}
		roleMap[spec.Protocol.Role] = spec.normalized()
	}

	return &Runtime{
		Kernel:    k,
		Sessions:  session.Catalog{Store: store, Checkpoints: k.Checkpoints()},
		Tasks:     tasks,
		Graph:     graph,
		Messages:  messageRuntime,
		Artifacts: k.ArtifactStore(),
		roles:     roleMap,
	}, nil
}

// RecoveryResolver exposes the kernel-side swarm recovery view.
func (r *Runtime) RecoveryResolver() kswarm.RecoveryResolver {
	if r == nil {
		return kswarm.RecoveryResolver{}
	}
	return kswarm.RecoveryResolver{
		Sessions:  r.Sessions,
		Tasks:     r.Graph,
		Messages:  r.Messages,
		Artifacts: r.Artifacts,
	}
}

// Snapshot reconstructs a swarm run from the current runtime facts.
func (r *Runtime) Snapshot(ctx context.Context, runID string, includeArchived bool) (*kswarm.Snapshot, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime must not be nil")
	}
	return r.RecoveryResolver().LoadRun(ctx, kswarm.RecoveryQuery{
		RunID:           runID,
		IncludeArchived: includeArchived,
	})
}

// EnsureRolePack registers the configured role pack into the subagent substrate.
func (r *Runtime) EnsureRolePack() error {
	if r == nil || r.Kernel == nil {
		return fmt.Errorf("runtime kernel is required")
	}
	return InstallRolePack(r.Kernel, r.RolePack()...)
}

// Role returns one role spec by role name.
func (r *Runtime) Role(role kswarm.Role) (RoleSpec, bool) {
	if r == nil {
		return RoleSpec{}, false
	}
	spec, ok := r.roles[role]
	return spec, ok
}

// RolePack returns all configured role specs in stable research-role order.
func (r *Runtime) RolePack() []RoleSpec {
	if r == nil {
		return nil
	}
	order := []kswarm.Role{
		kswarm.RolePlanner,
		kswarm.RoleSupervisor,
		kswarm.RoleWorker,
		kswarm.RoleSynthesizer,
		kswarm.RoleReviewer,
	}
	out := make([]RoleSpec, 0, len(r.roles))
	seen := make(map[kswarm.Role]struct{}, len(r.roles))
	for _, role := range order {
		if spec, ok := r.roles[role]; ok {
			out = append(out, spec)
			seen[role] = struct{}{}
		}
	}
	for role, spec := range r.roles {
		if _, ok := seen[role]; ok {
			continue
		}
		out = append(out, spec)
	}
	return out
}

// ResearchOrchestrator returns the default research-first orchestrator bound to
// this runtime.
func (r *Runtime) ResearchOrchestrator() (*ResearchOrchestrator, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime must not be nil")
	}
	return NewResearchOrchestrator(r)
}

// AttachRuntime stores a runtime adapter on the kernel service registry.
func AttachRuntime(k *kernel.Kernel, rt *Runtime) error {
	if k == nil {
		return fmt.Errorf("kernel must not be nil")
	}
	if rt == nil {
		return fmt.Errorf("runtime must not be nil")
	}
	k.Services().Store(runtimeServiceKey, rt)
	return nil
}

// RuntimeOf returns the swarm runtime stored on the kernel service registry.
func RuntimeOf(k *kernel.Kernel) *Runtime {
	if k == nil {
		return nil
	}
	value, ok := k.Services().Load(runtimeServiceKey)
	if !ok {
		return nil
	}
	rt, _ := value.(*Runtime)
	return rt
}

// RuntimeFeature installs the runtime adapter and default role pack after runtime setup.
func RuntimeFeature(specs ...RoleSpec) harness.Feature {
	copied := append([]RoleSpec(nil), specs...)
	return harness.FeatureFunc{
		FeatureName: "swarm-runtime",
		MetadataValue: harness.FeatureMetadata{
			Key:   "swarm-runtime",
			Phase: harness.FeaturePhasePostRuntime,
		},
		InstallFunc: func(_ context.Context, h *harness.Harness) error {
			if h == nil || h.Kernel() == nil {
				return fmt.Errorf("harness kernel is required")
			}
			if err := InstallEventBridges(h.Kernel()); err != nil {
				return err
			}
			rt, err := NewRuntime(h.Kernel(), copied...)
			if err != nil {
				return err
			}
			if err := rt.EnsureRolePack(); err != nil {
				return err
			}
			return AttachRuntime(h.Kernel(), rt)
		},
	}
}
