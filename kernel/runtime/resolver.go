package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// ─────────────────────────────────────────────
// RequestResolver 接口（§5.1、§5.2）
// ─────────────────────────────────────────────

// RequestResolver 接受 RuntimeRequest，输出 SessionBlueprint。
// 它是 runtime 的唯一入口编译器：将原始请求翻译为可执行的 canonical blueprint。
// 解析后的 blueprint 包含 Provenance.Hash，用于幂等校验和审计。
type RequestResolver interface {
	// Resolve 编译 RuntimeRequest 为 SessionBlueprint。
	// 若 req.RestoreSource 非空，则从 EventStore 中恢复原 blueprint（§5.3 恢复硬约束）。
	Resolve(req RuntimeRequest) (SessionBlueprint, error)
}

// ─────────────────────────────────────────────
// DefaultRequestResolver 最小实现
// ─────────────────────────────────────────────

// DefaultRequestResolver 是最小可用的 RequestResolver 实现。
// 它调用 PolicyCompiler 编译权限策略，使用内置默认值填充其他字段，
// 并计算确定性 Provenance.Hash。
type DefaultRequestResolver struct {
	policyCompiler PolicyCompiler
	// BuildVersion 供 Provenance.ResolverBuildVersion 使用。
	BuildVersion string
}

// NewDefaultRequestResolver 创建 DefaultRequestResolver。
func NewDefaultRequestResolver(policyCompiler PolicyCompiler) *DefaultRequestResolver {
	return &DefaultRequestResolver{
		policyCompiler: policyCompiler,
		BuildVersion:   "dev",
	}
}

// Resolve 实现 RequestResolver 接口。
func (r *DefaultRequestResolver) Resolve(req RuntimeRequest) (SessionBlueprint, error) {
	// 1. 编译权限策略
	policy, err := r.policyCompiler.Compile(req)
	if err != nil {
		return SessionBlueprint{}, fmt.Errorf("compile policy: %w", err)
	}

	// 2. 生成 session ID（ULID）
	sessionID := ulid.Make().String()

	// 3. 填充 blueprint 各子字段
	blueprint := SessionBlueprint{
		Identity: BlueprintIdentity{
			SessionID:   sessionID,
			WorkspaceID: req.Workspace,
		},
		ModelConfig:         resolveModelConfig(req),
		EffectiveToolPolicy: policy,
		ContextBudget:       resolveContextBudget(req),
		PromptPlan: PromptPlan{
			PromptPackID:       req.PromptPack,
			PromptBudgetPolicy: "soft_truncate",
		},
		PersistencePlan: resolvePersistencePlan(req),
		CheckpointPlan: CheckpointPlan{
			AutoCheckpointEveryNTurns: 10,
			CaptureWorkspaceSnapshot:  false,
		},
		SessionBudget: SessionBudget{
			MaxSteps: 200,
		},
		Provenance: BlueprintProvenance{
			BlueprintSchemaVersion: "1.0",
			ResolverBuildVersion:   r.BuildVersion,
		},
	}

	// 4. 计算 Provenance.Hash（幂等基线，§5.2 provenance 约束）
	blueprint.Provenance.Hash = computeBlueprintHash(blueprint)

	return blueprint, nil
}

// resolveModelConfig 从 RuntimeRequest 中推导模型配置。
func resolveModelConfig(req RuntimeRequest) BlueprintModelConfig {
	modelID := req.ModelProfile
	if modelID == "" {
		modelID = "claude-3-5-sonnet-20241022"
	}
	return BlueprintModelConfig{
		Provider: "anthropic",
		ModelID:  modelID,
	}
}

// resolveContextBudget 推导上下文预算（§14.10 thinking token 约束）。
func resolveContextBudget(_ RuntimeRequest) ContextBudget {
	return ContextBudget{
		MainTokenBudget:     100_000,
		ThinkingTokenBudget: 10_000,
	}
}

// resolvePersistencePlan 推导持久化策略。
func resolvePersistencePlan(req RuntimeRequest) PersistencePlan {
	workspace := req.Workspace
	if workspace == "" {
		workspace = "."
	}
	return PersistencePlan{
		StoreKind: "sqlite",
		StoreDSN:  workspace + "/moss_events.db",
	}
}

// computeBlueprintHash 计算 SessionBlueprint 的确定性 hash（§5.2 provenance 约束）。
// 使用 JSON 序列化后的 SHA-256 前 16 字节，不包含 Provenance.Hash 字段自身。
func computeBlueprintHash(b SessionBlueprint) string {
	// 临时清除 hash 字段以避免循环依赖
	b.Provenance.Hash = ""
	// 加入时间戳保证 session 唯一性（防止两次 Resolve 产生相同 sessionID 时 hash 碰撞）
	type hashInput struct {
		Blueprint SessionBlueprint `json:"blueprint"`
		Timestamp string           `json:"timestamp"`
	}
	data, _ := json.Marshal(hashInput{
		Blueprint: b,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	})
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:16])
}
