package providers

import "testing"

func TestBuildLLMOpenAIResponses(t *testing.T) {
	llm, err := BuildLLM("openai-responses", "gpt-5", "", "")
	if err != nil {
		t.Fatalf("BuildLLM(openai-responses): %v", err)
	}
	if llm == nil {
		t.Fatal("expected llm instance")
	}
}
