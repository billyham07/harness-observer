package model

import "testing"

func TestOpenAIInclusiveInputNormalization(t *testing.T) {
	u := NewInputIncludesCacheReadUsage(1000, 100, 900, 0, 40, nil, UsageObserved)
	if u.InputTokens != 100 || u.CacheReadTokens != 900 || u.PromptTokens != 1000 {
		t.Fatalf("unexpected normalized usage: %+v", u)
	}
	if u.NativeInputTokens == nil || *u.NativeInputTokens != 1000 {
		t.Fatalf("native input must be retained: %+v", u)
	}
}

// Codex token_count reports cache_write_input_tokens alongside cached input.
// Observed rollouts keep total_tokens == input_tokens + output_tokens, so cache
// writes are a subset of the native input counter, not an extra bucket.
func TestOpenAICacheWriteStaysInsideNativeInput(t *testing.T) {
	u := NewInputIncludesCacheReadUsage(1000, 100, 700, 200, 0, nil, UsageObserved)
	if u.InputTokens != 100 || u.CacheReadTokens != 700 || u.CacheWriteTokens != 200 {
		t.Fatalf("unexpected buckets: %+v", u)
	}
	if u.PromptTokens != 1000 {
		t.Fatalf("prompt tokens must stay equal to native input, got %d", u.PromptTokens)
	}
}

// A source that reports cache buckets larger than its own input counter is
// contradicting itself. Fresh input clamps to zero and the invariant
// prompt == input + cache_read + cache_write is what survives.
func TestOpenAIOverlargeCacheBucketsClamp(t *testing.T) {
	u := NewInputIncludesCacheReadUsage(100, 10, 300, 50, 0, nil, UsageObserved)
	if u.InputTokens != 0 {
		t.Fatalf("fresh input must clamp to zero, got %d", u.InputTokens)
	}
	if u.PromptTokens != u.InputTokens+u.CacheReadTokens+u.CacheWriteTokens {
		t.Fatalf("prompt invariant broken: %+v", u)
	}
}

func TestSplitUsagePromptTokens(t *testing.T) {
	total := int64(555)
	u := NewSplitUsage(100, 20, 80, 5, 0, &total, UsageObserved)
	if u.PromptTokens != 185 || u.InputTokens != 100 {
		t.Fatalf("unexpected normalized usage: %+v", u)
	}
	if u.NativeTotalTokens == nil || *u.NativeTotalTokens != total {
		t.Fatalf("native total must be retained: %+v", u)
	}
}

// Reasoning is a subset of output in every verified source format, so the
// mechanical observed total must not add it a second time.
func TestObservedTokensExcludesReasoning(t *testing.T) {
	turn := Turn{Usage: NewSplitUsage(100, 500, 0, 0, 80, nil, UsageObserved)}
	if got := turn.ObservedTokens(); got != 600 {
		t.Fatalf("observed tokens = %d, want 600 (prompt 100 + output 500)", got)
	}
}
