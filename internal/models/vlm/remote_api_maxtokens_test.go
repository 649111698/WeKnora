package vlm

import (
	"testing"
)

// TestRemoteAPIVLMMaxTokensOverride 验证 extra_config.max_tokens 覆盖默认值
//（智谱 glm-4v-flash 限制 max_tokens ∈ [1,1024]，默认 5000 会被 400 拒绝）。
func TestRemoteAPIVLMMaxTokensOverride(t *testing.T) {
	cfg := &Config{
		ModelName: "glm-4v-flash",
		APIKey:    "test-key",
		BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
		Provider:  "zhipu",
		Extra: map[string]any{
			"max_tokens": "1024",
		},
	}
	v, err := NewRemoteAPIVLM(cfg)
	if err != nil {
		t.Fatalf("NewRemoteAPIVLM failed: %v", err)
	}
	if v.maxTokens != 1024 {
		t.Fatalf("maxTokens = %d, want 1024", v.maxTokens)
	}

	// 未配置 / 非法值回退默认
	v2, err := NewRemoteAPIVLM(&Config{
		ModelName: "some-vlm",
		APIKey:    "test-key",
		BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
		Extra: map[string]any{
			"max_tokens": "not-a-number",
		},
	})
	if err != nil {
		t.Fatalf("NewRemoteAPIVLM failed: %v", err)
	}
	if v2.maxTokens != defaultMaxToks {
		t.Fatalf("maxTokens = %d, want default %d", v2.maxTokens, defaultMaxToks)
	}
}
