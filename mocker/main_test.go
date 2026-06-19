package main

import (
	"testing"
	"time"
)

func TestProviderAliasesCoverConfiguredProviders(t *testing.T) {
	requiredProviders := []string{
		"openai", "anthropic", "bedrock", "gemini", "vertex", "cohere", "xai", "groq",
		"perplexity", "cerebras", "mistral", "elevenlabs", "azure", "huggingface",
		"ollama", "openrouter", "parasail", "replicate", "sgl", "vllm",
	}

	for _, provider := range requiredProviders {
		parsedProvider, parsedModel := parseProviderAndModel(provider + "/test-model")
		if parsedProvider == "" {
			t.Fatalf("provider %q is not recognized in provider/model format", provider)
		}
		if parsedModel != "test-model" {
			t.Fatalf("provider %q parsed wrong model: got %q, want %q", provider, parsedModel, "test-model")
		}
	}
}

func TestProviderErrorCatalogCoverage(t *testing.T) {
	requiredProviders := []string{
		"openai", "anthropic", "bedrock", "gemini", "vertex", "cohere", "xai", "groq",
		"perplexity", "cerebras", "mistral", "elevenlabs", "azure", "huggingface",
		"ollama", "openrouter", "parasail", "replicate", "sgl", "vllm",
	}
	for _, provider := range requiredProviders {
		if len(providerErrorCatalog(provider)) == 0 {
			t.Fatalf("provider %q has no error variants", provider)
		}
	}
}

func TestParseBedrockModelFromPath(t *testing.T) {
	cases := []struct {
		path       string
		model      string
		isConverse bool
		isStream   bool
	}{
		{"/model/amazon.nova-micro-v1:0/converse", "amazon.nova-micro-v1:0", true, false},
		{"/model/amazon.nova-micro-v1%3A0/converse-stream", "amazon.nova-micro-v1%3A0", true, true},
		{"/bedrock/model/anthropic.claude-3-5-sonnet-20240620-v1:0/converse", "anthropic.claude-3-5-sonnet-20240620-v1:0", true, false},
		{"/v1/chat/completions", "", false, false},
	}
	for _, tc := range cases {
		model, isConverse, isStream := parseBedrockModelFromPath(tc.path)
		if model != tc.model || isConverse != tc.isConverse || isStream != tc.isStream {
			t.Fatalf("parseBedrockModelFromPath(%q) = (%q,%v,%v), want (%q,%v,%v)",
				tc.path, model, isConverse, isStream, tc.model, tc.isConverse, tc.isStream)
		}
	}
}

func TestParseGenAIModelFromPath(t *testing.T) {
	cases := []struct {
		path     string
		provider string
		model    string
	}{
		{"/models/gemini-3-pro-preview:streamGenerateContent", "gemini", "gemini-3-pro-preview"},
		{"/v1beta/models/gemini-2.0-flash:generateContent", "gemini", "gemini-2.0-flash"},
		{"/genai/v1beta/models/vertex%2Fgemini-2.0-flash:generateContent", "vertex", "gemini-2.0-flash"},
	}
	for _, tc := range cases {
		provider, model := parseGenAIModelFromPath(tc.path)
		if provider != tc.provider || model != tc.model {
			t.Fatalf("parseGenAIModelFromPath(%q) = (%q,%q), want (%q,%q)",
				tc.path, provider, model, tc.provider, tc.model)
		}
	}
}

func TestEffectiveFailurePercentWithErrorsKeepsMix(t *testing.T) {
	prevWithErrors := withErrors
	prevFailurePercent := failurePercent
	prevFailureJitter := failureJitter
	defer func() {
		withErrors = prevWithErrors
		failurePercent = prevFailurePercent
		failureJitter = prevFailureJitter
	}()

	withErrors = true
	failureJitter = 0

	failurePercent = 0
	if got := effectiveFailurePercent(); got != 20 {
		t.Fatalf("effectiveFailurePercent() with withErrors+0 = %d, want 20", got)
	}
	failurePercent = 100
	if got := effectiveFailurePercent(); got != 95 {
		t.Fatalf("effectiveFailurePercent() with withErrors+100 = %d, want 95", got)
	}
}

func TestShouldFailDisabledWhenWithErrorsEnabled(t *testing.T) {
	prevWithErrors := withErrors
	prevFailurePercent := failurePercent
	defer func() {
		withErrors = prevWithErrors
		failurePercent = prevFailurePercent
	}()

	withErrors = true
	failurePercent = 100
	if shouldFail("") {
		t.Fatalf("shouldFail() must be false when withErrors is enabled")
	}
}

func TestAuthKeyMatches(t *testing.T) {
	cases := []struct {
		keysCSV    string
		authHeader string
		want       bool
	}{
		{"", "Bearer anything", true},
		{"", "", true},
		{"key-a", "Bearer key-a", true},
		{"key-a", "key-a", true},
		{"key-a,key-b", "Bearer key-b", true},
		{" key-a , key-b ", "Bearer key-b", true},
		{"key-a", "Bearer key-c", false},
		{"key-a", "", false},
	}
	for _, c := range cases {
		if got := authKeyMatches(c.keysCSV, c.authHeader); got != c.want {
			t.Errorf("authKeyMatches(%q, %q) = %v, want %v", c.keysCSV, c.authHeader, got, c.want)
		}
	}
}

func TestShouldFailScopedToFailureAuthKeys(t *testing.T) {
	prevFailurePercent := failurePercent
	prevFailureAuthKeys := failureAuthKeys
	prevWithErrors := withErrors
	defer func() {
		failurePercent = prevFailurePercent
		failureAuthKeys = prevFailureAuthKeys
		withErrors = prevWithErrors
	}()

	withErrors = false
	failurePercent = 100
	failureAuthKeys = "failing-key"
	if !shouldFail("Bearer failing-key") {
		t.Fatalf("shouldFail() must be true for a listed key at 100%% failure")
	}
	if shouldFail("Bearer healthy-key") {
		t.Fatalf("shouldFail() must be false for a key not in failure-auth-keys")
	}
}

func TestStreamLatencyScopedToLatencyAuthKeys(t *testing.T) {
	prevLatency := latency
	prevJitter := jitter
	prevLatencyAuthKeys := latencyAuthKeys
	defer func() {
		latency = prevLatency
		jitter = prevJitter
		latencyAuthKeys = prevLatencyAuthKeys
	}()

	latency = 5000
	jitter = 0
	latencyAuthKeys = "slow-key"
	if got := getStreamTotalLatency("Bearer slow-key"); got != 5*time.Second {
		t.Fatalf("getStreamTotalLatency(slow-key) = %v, want 5s", got)
	}
	if got := getStreamTotalLatency("Bearer fast-key"); got != 0 {
		t.Fatalf("getStreamTotalLatency(fast-key) = %v, want 0", got)
	}
}

func TestParseLatencyEntry(t *testing.T) {
	cases := []struct {
		entry   string
		key     string
		spec    latencySpec
		hasSpec bool
	}{
		{"key-a", "key-a", latencySpec{}, false},
		{"key-a=200", "key-a", latencySpec{latencyMs: 200}, true},
		{"key-a=200:50", "key-a", latencySpec{latencyMs: 200, jitterMs: 50}, true},
		{"key-a=0", "key-a", latencySpec{}, true},
		// '=' in the token (base64 padding) with no numeric suffix stays a bare key.
		{"a2V5LWE=", "a2V5LWE=", latencySpec{}, false},
		// '=' in the token plus a numeric override splits at the last '='.
		{"a2V5LWE==300", "a2V5LWE=", latencySpec{latencyMs: 300}, true},
		// Malformed overrides degrade to bare keys.
		{"key-a=abc", "key-a=abc", latencySpec{}, false},
		{"key-a=200:xy", "key-a=200:xy", latencySpec{}, false},
		{"key-a=-5", "key-a=-5", latencySpec{}, false},
	}
	for _, c := range cases {
		key, spec, hasSpec := parseLatencyEntry(c.entry)
		if key != c.key || spec != c.spec || hasSpec != c.hasSpec {
			t.Errorf("parseLatencyEntry(%q) = (%q, %+v, %v), want (%q, %+v, %v)",
				c.entry, key, spec, hasSpec, c.key, c.spec, c.hasSpec)
		}
	}
}

func TestResolveLatencySpecPerKeyOverrides(t *testing.T) {
	prevLatency := latency
	prevJitter := jitter
	defer func() {
		latency = prevLatency
		jitter = prevJitter
	}()

	latency = 1000
	jitter = 100
	keys := "key-a=200:50, key-b=800, key-c"

	spec, ok := resolveLatencySpec(keys, "Bearer key-a")
	if !ok || spec != (latencySpec{latencyMs: 200, jitterMs: 50}) {
		t.Fatalf("resolveLatencySpec(key-a) = (%+v, %v), want override 200:50", spec, ok)
	}
	spec, ok = resolveLatencySpec(keys, "Bearer key-b")
	if !ok || spec != (latencySpec{latencyMs: 800}) {
		t.Fatalf("resolveLatencySpec(key-b) = (%+v, %v), want override 800:0", spec, ok)
	}
	spec, ok = resolveLatencySpec(keys, "Bearer key-c")
	if !ok || spec != (latencySpec{latencyMs: 1000, jitterMs: 100}) {
		t.Fatalf("resolveLatencySpec(key-c) = (%+v, %v), want global 1000:100", spec, ok)
	}
	if _, ok = resolveLatencySpec(keys, "Bearer key-d"); ok {
		t.Fatalf("resolveLatencySpec(key-d) matched, want no match")
	}
	spec, ok = resolveLatencySpec("", "Bearer anything")
	if !ok || spec != (latencySpec{latencyMs: 1000, jitterMs: 100}) {
		t.Fatalf("resolveLatencySpec(empty list) = (%+v, %v), want global for all", spec, ok)
	}
}

func TestParseFailureEntry(t *testing.T) {
	cases := []struct {
		entry   string
		key     string
		spec    failureSpec
		hasSpec bool
	}{
		{"key-a", "key-a", failureSpec{}, false},
		{"key-a=10", "key-a", failureSpec{percent: 10}, true},
		{"key-a=10:5", "key-a", failureSpec{percent: 10, jitter: 5}, true},
		{"key-a=0", "key-a", failureSpec{}, true},
		// '=' in the token (base64 padding) with no numeric suffix stays a bare key.
		{"a2V5LWE=", "a2V5LWE=", failureSpec{}, false},
		// '=' in the token plus a numeric override splits at the last '='.
		{"a2V5LWE==30", "a2V5LWE=", failureSpec{percent: 30}, true},
		// Malformed overrides degrade to bare keys.
		{"key-a=abc", "key-a=abc", failureSpec{}, false},
		{"key-a=10:xy", "key-a=10:xy", failureSpec{}, false},
		{"key-a=-5", "key-a=-5", failureSpec{}, false},
	}
	for _, c := range cases {
		key, spec, hasSpec := parseFailureEntry(c.entry)
		if key != c.key || spec != c.spec || hasSpec != c.hasSpec {
			t.Errorf("parseFailureEntry(%q) = (%q, %+v, %v), want (%q, %+v, %v)",
				c.entry, key, spec, hasSpec, c.key, c.spec, c.hasSpec)
		}
	}
}

func TestResolveFailureSpecPerKeyOverrides(t *testing.T) {
	prevFailurePercent := failurePercent
	prevFailureJitter := failureJitter
	defer func() {
		failurePercent = prevFailurePercent
		failureJitter = prevFailureJitter
	}()

	failurePercent = 50
	failureJitter = 5
	keys := "key-a=10:2, key-b=20, key-c"

	spec, ok := resolveFailureSpec(keys, "Bearer key-a")
	if !ok || spec != (failureSpec{percent: 10, jitter: 2}) {
		t.Fatalf("resolveFailureSpec(key-a) = (%+v, %v), want override 10:2", spec, ok)
	}
	spec, ok = resolveFailureSpec(keys, "Bearer key-b")
	if !ok || spec != (failureSpec{percent: 20}) {
		t.Fatalf("resolveFailureSpec(key-b) = (%+v, %v), want override 20:0", spec, ok)
	}
	spec, ok = resolveFailureSpec(keys, "Bearer key-c")
	if !ok || spec != (failureSpec{percent: 50, jitter: 5}) {
		t.Fatalf("resolveFailureSpec(key-c) = (%+v, %v), want global 50:5", spec, ok)
	}
	if _, ok = resolveFailureSpec(keys, "Bearer key-d"); ok {
		t.Fatalf("resolveFailureSpec(key-d) matched, want no match")
	}
	spec, ok = resolveFailureSpec("", "Bearer anything")
	if !ok || spec != (failureSpec{percent: 50, jitter: 5}) {
		t.Fatalf("resolveFailureSpec(empty list) = (%+v, %v), want global for all", spec, ok)
	}
}

func TestShouldFailPerKeyOverrides(t *testing.T) {
	prevFailurePercent := failurePercent
	prevFailureJitter := failureJitter
	prevFailureAuthKeys := failureAuthKeys
	prevWithErrors := withErrors
	defer func() {
		failurePercent = prevFailurePercent
		failureJitter = prevFailureJitter
		failureAuthKeys = prevFailureAuthKeys
		withErrors = prevWithErrors
	}()

	withErrors = false
	failurePercent = 0
	failureJitter = 0
	// slow-key never fails (0%), fast-key always fails (100%), unlisted keys succeed.
	failureAuthKeys = "slow-key=0, fast-key=100"

	for i := 0; i < 50; i++ {
		if shouldFail("Bearer slow-key") {
			t.Fatalf("shouldFail(slow-key) must be false at 0%% override")
		}
		if !shouldFail("Bearer fast-key") {
			t.Fatalf("shouldFail(fast-key) must be true at 100%% override")
		}
		if shouldFail("Bearer other-key") {
			t.Fatalf("shouldFail(other-key) must be false when not listed")
		}
	}
}

func TestResolveTokensFixedAndFallback(t *testing.T) {
	prevInput := fixedInputTokens
	prevOutput := fixedOutputTokens
	defer func() {
		fixedInputTokens = prevInput
		fixedOutputTokens = prevOutput
	}()

	// Negative sentinel = use the fallback (random/derived) value.
	fixedInputTokens = -1
	fixedOutputTokens = -1
	if got := resolveInputTokens(123); got != 123 {
		t.Fatalf("resolveInputTokens(123) with no override = %d, want 123", got)
	}
	if got := resolveOutputTokens(456); got != 456 {
		t.Fatalf("resolveOutputTokens(456) with no override = %d, want 456", got)
	}

	// Configured values (including 0) override the fallback.
	fixedInputTokens = 100
	fixedOutputTokens = 0
	if got := resolveInputTokens(123); got != 100 {
		t.Fatalf("resolveInputTokens(123) with override 100 = %d, want 100", got)
	}
	if got := resolveOutputTokens(456); got != 0 {
		t.Fatalf("resolveOutputTokens(456) with override 0 = %d, want 0", got)
	}
}

func TestStreamLatencyPerKeyOverride(t *testing.T) {
	prevLatency := latency
	prevJitter := jitter
	prevLatencyAuthKeys := latencyAuthKeys
	defer func() {
		latency = prevLatency
		jitter = prevJitter
		latencyAuthKeys = prevLatencyAuthKeys
	}()

	latency = 5000
	jitter = 0
	latencyAuthKeys = "slow-key=2000,default-key"
	if got := getStreamTotalLatency("Bearer slow-key"); got != 2*time.Second {
		t.Fatalf("getStreamTotalLatency(slow-key) = %v, want 2s", got)
	}
	if got := getStreamTotalLatency("Bearer default-key"); got != 5*time.Second {
		t.Fatalf("getStreamTotalLatency(default-key) = %v, want 5s", got)
	}
	if got := getStreamTotalLatency("Bearer fast-key"); got != 0 {
		t.Fatalf("getStreamTotalLatency(fast-key) = %v, want 0", got)
	}
}
