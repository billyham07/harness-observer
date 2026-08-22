package model

import "time"

// UsageBasis describes how the per-event token counts were obtained.
// Keep this independent from provider/harness so a dashboard can filter out
// estimates or cumulative-delta reconstructions when doing strict comparisons.
type UsageBasis string

const (
	UsageObserved     UsageBasis = "observed"
	UsageDerivedDelta UsageBasis = "derived-delta"
	UsageEstimated    UsageBasis = "estimated"
)

// UsageSemantics records the native accounting shape that was normalized.
type UsageSemantics string

const (
	// SplitCache means the source already reports fresh input, cache reads and
	// cache writes as disjoint buckets (Anthropic and Pi/OMP normalized usage).
	UsageSplitCache UsageSemantics = "split-cache"
	// InputIncludesCacheRead means the source's input counter contains cache
	// reads as a subset (Codex/OpenAI token_count).
	UsageInputIncludesCacheRead UsageSemantics = "input-includes-cache-read"
)

// Usage uses one canonical accounting model across harnesses:
//
//	input_tokens      = fresh/uncached prompt input
//	cache_read_tokens = prompt tokens served from cache
//	cache_write_tokens= prompt tokens written to cache
//	prompt_tokens     = input + cache_read + cache_write
//
// output_tokens and reasoning_tokens stay separate when the source exposes
// reasoning separately. If a source does not expose reasoning, it remains 0;
// we never infer it from text length.
//
// NativeInputTokens/NativeTotalTokens preserve authoritative source counters
// when their semantics differ from our normalized representation. Pointers are
// intentional: nil means "not reported", which is different from a real zero.
type Usage struct {
	InputTokens       int64          `json:"input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	CacheReadTokens   int64          `json:"cache_read_tokens"`
	CacheWriteTokens  int64          `json:"cache_write_tokens"`
	ReasoningTokens   int64          `json:"reasoning_tokens"`
	PromptTokens      int64          `json:"prompt_tokens"`
	NativeInputTokens *int64         `json:"native_input_tokens,omitempty"`
	NativeTotalTokens *int64         `json:"native_total_tokens,omitempty"`
	Semantics         UsageSemantics `json:"semantics"`
	Basis             UsageBasis     `json:"basis"`
}

// NewSplitUsage normalizes a source whose input/cache buckets are already
// disjoint (Claude and Pi/OMP). nativeTotal may be nil when the source has no
// authoritative total counter.
func NewSplitUsage(input, output, cacheRead, cacheWrite, reasoning int64, nativeTotal *int64, basis UsageBasis) Usage {
	input = nonNegative(input)
	output = nonNegative(output)
	cacheRead = nonNegative(cacheRead)
	cacheWrite = nonNegative(cacheWrite)
	reasoning = nonNegative(reasoning)
	nativeInput := input
	return Usage{
		InputTokens:       input,
		OutputTokens:      output,
		CacheReadTokens:   cacheRead,
		CacheWriteTokens:  cacheWrite,
		ReasoningTokens:   reasoning,
		PromptTokens:      input + cacheRead + cacheWrite,
		NativeInputTokens: &nativeInput,
		NativeTotalTokens: nativeTotal,
		Semantics:         UsageSplitCache,
		Basis:             basis,
	}
}

// NewInputIncludesCacheReadUsage normalizes OpenAI/Codex accounting where the
// native input counter already includes cached input. Codex token_count also
// reports cache_write_input_tokens; observed rollouts satisfy
//
//	total_tokens == input_tokens + output_tokens
//
// so cache writes are treated as a subset of the native input counter rather
// than an extra bucket added on top of it. That keeps both invariants true:
//
//	prompt_tokens == native input_tokens
//	prompt_tokens == input + cache_read + cache_write
//
// See docs/usage-semantics.md for the evidence and the fallback when a source
// reports cache buckets larger than its own input counter.
func NewInputIncludesCacheReadUsage(nativeInput, output, cacheRead, cacheWrite, reasoning int64, nativeTotal *int64, basis UsageBasis) Usage {
	nativeInput = nonNegative(nativeInput)
	output = nonNegative(output)
	cacheRead = nonNegative(cacheRead)
	cacheWrite = nonNegative(cacheWrite)
	reasoning = nonNegative(reasoning)
	fresh := nativeInput - cacheRead - cacheWrite
	if fresh < 0 {
		fresh = 0
	}
	return Usage{
		InputTokens:       fresh,
		OutputTokens:      output,
		CacheReadTokens:   cacheRead,
		CacheWriteTokens:  cacheWrite,
		ReasoningTokens:   reasoning,
		PromptTokens:      fresh + cacheRead + cacheWrite,
		NativeInputTokens: &nativeInput,
		NativeTotalTokens: nativeTotal,
		Semantics:         UsageInputIncludesCacheRead,
		Basis:             basis,
	}
}

func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

type Turn struct {
	Timestamp time.Time `json:"timestamp"`
	Harness   string    `json:"harness"`
	Client    string    `json:"client,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	// ReasoningEffort is the thinking/effort level the harness persisted for
	// this call. Empty means the source did not record one; it is never
	// inferred from reasoning-token volume or from the model name.
	ReasoningEffort string   `json:"reasoning_effort,omitempty"`
	Project         string   `json:"project,omitempty"`
	CWD             string   `json:"cwd,omitempty"`
	SessionID       string   `json:"session_id"`
	RecordID        string   `json:"record_id"`
	TurnID          string   `json:"turn_id,omitempty"`
	EventKind       string   `json:"event_kind,omitempty"`
	ToolCalls       int      `json:"tool_calls"`
	ToolErrors      int      `json:"tool_errors"`
	Usage           Usage    `json:"usage"`
	CostUSD         *float64 `json:"cost_usd,omitempty"`
	SourceFile      string   `json:"source_file"`
	SourceLine      int      `json:"source_line"`
}

// ObservedTokens is a mechanical sum of normalized buckets, useful for local
// throughput/accounting charts.
//
// ReasoningTokens is deliberately excluded: in every source format verified so
// far reasoning is a subset of the output counter, not an additional bucket.
// Codex rollouts report total_tokens == input_tokens + output_tokens while
// reasoning_output_tokens is smaller than output_tokens, and Anthropic reports
// output_tokens_details.thinking_tokens inside output_tokens. Adding reasoning
// here would double count it.
func (t Turn) ObservedTokens() int64 {
	return t.Usage.PromptTokens + t.Usage.OutputTokens
}
