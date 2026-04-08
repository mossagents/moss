package model

// ModelCapability 表示模型具备的能力标签。
type ModelCapability string

const (
	CapTextGeneration     ModelCapability = "text_generation"
	CapCodeGeneration     ModelCapability = "code_generation"
	CapImageGeneration    ModelCapability = "image_generation"
	CapImageUnderstanding ModelCapability = "image_understanding"
	CapAudioGeneration    ModelCapability = "audio_generation"
	CapAudioUnderstanding ModelCapability = "audio_understanding"
	CapVideoGeneration    ModelCapability = "video_generation"
	CapVideoUnderstanding ModelCapability = "video_understanding"
	CapReasoning          ModelCapability = "reasoning"
	CapFunctionCalling    ModelCapability = "function_calling"
	CapLongContext        ModelCapability = "long_context"
)

// TaskRequirement 描述一个任务对模型的需求。
// 由调用方设置在 ModelConfig.Requirements 中，供 ModelRouter 选择最优模型。
type TaskRequirement struct {
	// Capabilities 任务所需的模型能力列表，所有能力必须同时满足。
	Capabilities []ModelCapability `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`

	// MaxCostTier 允许的最高成本等级（1=低, 2=中, 3=高），0 表示不限。
	MaxCostTier int `json:"max_cost_tier,omitempty" yaml:"max_cost_tier,omitempty"`

	// PreferCheap 在能力满足的前提下，优先选择成本更低的模型。
	PreferCheap bool `json:"prefer_cheap,omitempty" yaml:"prefer_cheap,omitempty"`

	// Lane 表示请求希望命中的模型路由通道，例如 default/cheap/reasoning/tool-heavy。
	Lane string `json:"lane,omitempty" yaml:"lane,omitempty"`
}
