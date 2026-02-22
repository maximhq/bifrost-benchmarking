package main

import "testing"

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
	if shouldFail() {
		t.Fatalf("shouldFail() must be false when withErrors is enabled")
	}
}
