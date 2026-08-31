package main

import (
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
)

// withRouterConfig applies a model router configuration for the duration of a
// test and restores whatever was there before.
func withRouterConfig(t *testing.T, names string, models string, strategy string) {
	t.Helper()
	prevNames, prevModels, prevStrategy := modelRouterNames, modelRouterModels, modelRouterStrategy
	prevCandidates, prevTotal, prevSet := modelRouterCandidates, modelRouterTotalWeight, modelRouterNameSet
	prevNameList := modelRouterNameList
	t.Cleanup(func() {
		modelRouterNames, modelRouterModels, modelRouterStrategy = prevNames, prevModels, prevStrategy
		modelRouterCandidates, modelRouterTotalWeight, modelRouterNameSet = prevCandidates, prevTotal, prevSet
		modelRouterNameList = prevNameList
		modelRouterCursor.Store(0)
	})

	modelRouterNames, modelRouterModels, modelRouterStrategy = names, models, strategy
	modelRouterCursor.Store(0)
	configureModelRouter()
}

// routerCtx builds a request context whose body is bodySize bytes, which is what
// the prompt-size strategy keys off.
func routerCtx(bodySize int) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.SetBody([]byte(strings.Repeat("x", bodySize)))
	return ctx
}

func TestParseAzureDeployment(t *testing.T) {
	cases := []struct {
		path       string
		deployment string
		endpoint   string
		ok         bool
	}{
		{"/openai/deployments/model-router/chat/completions", "model-router", "chat/completions", true},
		{"/azure/openai/deployments/model-router/chat/completions", "model-router", "chat/completions", true},
		{"/openai/deployments/my%2Frouter/embeddings", "my/router", "embeddings", true},
		{"/openai/deployments/model-router/responses", "model-router", "responses", true},
		{"/openai/deployments/model-router", "", "", false},
		{"/v1/chat/completions", "", "", false},
	}
	for _, tc := range cases {
		deployment, endpoint, ok := parseAzureDeployment(tc.path)
		if deployment != tc.deployment || endpoint != tc.endpoint || ok != tc.ok {
			t.Fatalf("parseAzureDeployment(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.path, deployment, endpoint, ok, tc.deployment, tc.endpoint, tc.ok)
		}
	}
}

func TestConfigureModelRouterParsesWeights(t *testing.T) {
	withRouterConfig(t, "model-router, Router-B", "gpt-5-nano=3,gpt-5-mini,gpt-5=6", "random")

	want := []routerCandidate{{"gpt-5-nano", 3}, {"gpt-5-mini", 1}, {"gpt-5", 6}}
	if len(modelRouterCandidates) != len(want) {
		t.Fatalf("parsed %d candidates, want %d", len(modelRouterCandidates), len(want))
	}
	for i, candidate := range want {
		if modelRouterCandidates[i] != candidate {
			t.Fatalf("candidate %d = %+v, want %+v", i, modelRouterCandidates[i], candidate)
		}
	}
	if modelRouterTotalWeight != 10 {
		t.Fatalf("total weight = %d, want 10", modelRouterTotalWeight)
	}
	if !isModelRouter("model-router") || !isModelRouter("router-b") {
		t.Fatalf("configured router names must be recognized case-insensitively")
	}
	if isModelRouter("gpt-5-mini") {
		t.Fatalf("a concrete model must not be treated as a router")
	}
	if len(modelRouterNameList) != 2 || modelRouterNameList[0] != "model-router" || modelRouterNameList[1] != "router-b" {
		t.Fatalf("router names must keep their configured order, got %v", modelRouterNameList)
	}
}

func TestResolveRoutedModelPassesThroughConcreteModels(t *testing.T) {
	withRouterConfig(t, "model-router", "gpt-5-nano", "random")

	model, routed := resolveRoutedModel(routerCtx(0), "gpt-4o-mini")
	if routed || model != "gpt-4o-mini" {
		t.Fatalf("resolveRoutedModel(gpt-4o-mini) = (%q,%v), want (gpt-4o-mini,false)", model, routed)
	}

	model, routed = resolveRoutedModel(routerCtx(0), "model-router")
	if !routed || model != "gpt-5-nano" {
		t.Fatalf("resolveRoutedModel(model-router) = (%q,%v), want (gpt-5-nano,true)", model, routed)
	}
}

func TestResolveRoutedModelDisabledWithoutCandidates(t *testing.T) {
	withRouterConfig(t, "model-router", "", "random")

	model, routed := resolveRoutedModel(routerCtx(0), "model-router")
	if routed || model != "model-router" {
		t.Fatalf("with no candidates the router name must pass through, got (%q,%v)", model, routed)
	}
}

func TestSelectRoutedModelRoundRobin(t *testing.T) {
	withRouterConfig(t, "model-router", "a,b,c", "round-robin")

	want := []string{"a", "b", "c", "a", "b"}
	for i, expected := range want {
		if got := selectRoutedModel(0); got != expected {
			t.Fatalf("round-robin pick %d = %q, want %q", i, got, expected)
		}
	}
}

func TestSelectRoutedModelPromptSizeEscalates(t *testing.T) {
	withRouterConfig(t, "model-router", "nano,mini,full", "prompt-size")

	cases := []struct {
		bodySize int
		want     string
	}{
		{0, "nano"},
		{1023, "nano"},
		{1024, "mini"},
		{4095, "mini"},
		{4096, "full"},
		{1 << 20, "full"},
	}
	for _, tc := range cases {
		if got := selectRoutedModel(tc.bodySize); got != tc.want {
			t.Fatalf("prompt-size pick for %d bytes = %q, want %q", tc.bodySize, got, tc.want)
		}
	}
}

func TestSelectRoutedModelWeightedRandomRespectsWeights(t *testing.T) {
	withRouterConfig(t, "model-router", "cheap=9,expensive=1", "random")

	counts := map[string]int{}
	for i := 0; i < 5000; i++ {
		counts[selectRoutedModel(0)]++
	}
	if counts["cheap"]+counts["expensive"] != 5000 {
		t.Fatalf("weighted picks must only return configured models, got %v", counts)
	}
	if counts["expensive"] == 0 {
		t.Fatalf("every weighted candidate must be reachable, got %v", counts)
	}
	// 9:1 weights: cheap should dominate by a wide margin even allowing for noise.
	if counts["cheap"] <= counts["expensive"]*3 {
		t.Fatalf("weights not respected: %v", counts)
	}
}

func TestAzureDeploymentModelFeedsParsedModel(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	if got := azureDeploymentModel(ctx); got != "" {
		t.Fatalf("azureDeploymentModel on a non-Azure route = %q, want empty", got)
	}

	// Azure clients omit "model" from the body; the deployment in the path wins.
	ctx.Request.SetBody([]byte(`{"messages":[]}`))
	ctx.SetUserValue(azureDeploymentKey, "model-router")
	provider, model, stream := parseModelFromRequest(ctx)
	if provider != "azure" || model != "model-router" || stream {
		t.Fatalf("parseModelFromRequest = (%q,%q,%v), want (azure,model-router,false)", provider, model, stream)
	}

	// An explicit body model still takes precedence over the path.
	ctx.Request.SetBody([]byte(`{"model":"gpt-4o","stream":true}`))
	provider, model, stream = parseModelFromRequest(ctx)
	if provider != "" || model != "gpt-4o" || !stream {
		t.Fatalf("parseModelFromRequest = (%q,%q,%v), want (\"\",gpt-4o,true)", provider, model, stream)
	}
}
