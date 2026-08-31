package main

// Azure AI Foundry ships a "model router" deployment: callers address one
// deployment name and the service picks an underlying model per request,
// reporting the winner in the response's `model` field. This file mocks both
// halves of that — the Azure deployment-style routes and the per-request model
// selection — so a gateway can be exercised against responses whose `model`
// differs from the one it asked for.

import (
	"log"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/valyala/fasthttp"
)

const (
	// azureDeploymentKey holds the deployment parsed out of the request path,
	// so the shared handlers can use it as the model.
	azureDeploymentKey = "azure_deployment"
	// azureDeploymentsSegment precedes the deployment name in Azure OpenAI URLs.
	azureDeploymentsSegment = "deployments"
)

// routerCandidate is one model the router may pick, with its relative weight.
type routerCandidate struct {
	model  string
	weight int
}

var (
	modelRouterCandidates  []routerCandidate
	modelRouterTotalWeight int
	modelRouterNameSet     = map[string]bool{}
	// modelRouterNameList keeps the configured order for logs and /v1/models.
	modelRouterNameList []string
	modelRouterCursor   atomic.Uint64
)

// configureModelRouter turns -model-router-names / -model-router-models into the
// lookup structures consulted per request. Called once from main().
func configureModelRouter() {
	modelRouterCandidates = nil
	modelRouterTotalWeight = 0
	modelRouterNameSet = map[string]bool{}
	modelRouterNameList = nil

	for name := range strings.SplitSeq(modelRouterNames, ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" && !modelRouterNameSet[name] {
			modelRouterNameSet[name] = true
			modelRouterNameList = append(modelRouterNameList, name)
		}
	}
	for entry := range strings.SplitSeq(modelRouterModels, ",") {
		modelName, weightStr, hasWeight := strings.Cut(strings.TrimSpace(entry), "=")
		modelName = strings.TrimSpace(modelName)
		if modelName == "" {
			continue
		}
		weight := 1
		if hasWeight {
			parsed, err := strconv.Atoi(strings.TrimSpace(weightStr))
			if err != nil || parsed < 1 {
				log.Printf("Ignoring invalid model router weight in %q, defaulting to 1", entry)
			} else {
				weight = parsed
			}
		}
		modelRouterCandidates = append(modelRouterCandidates, routerCandidate{model: modelName, weight: weight})
		modelRouterTotalWeight += weight
	}

	if len(modelRouterNameSet) == 0 || len(modelRouterCandidates) == 0 {
		return
	}
	picks := make([]string, 0, len(modelRouterCandidates))
	for _, candidate := range modelRouterCandidates {
		picks = append(picks, candidate.model)
	}
	log.Printf("Model router enabled for %s (%s over %s)",
		strings.Join(modelRouterNameList, ", "), modelRouterStrategy, strings.Join(picks, ", "))
}

// isModelRouter reports whether model addresses a router deployment rather than
// a concrete model.
func isModelRouter(model string) bool {
	return modelRouterNameSet[strings.ToLower(strings.TrimSpace(model))]
}

// resolveRoutedModel returns the underlying model the router serves this request
// with, and whether routing applied at all. Concrete models pass through.
func resolveRoutedModel(ctx *fasthttp.RequestCtx, model string) (string, bool) {
	if len(modelRouterCandidates) == 0 || !isModelRouter(model) {
		return model, false
	}
	return selectRoutedModel(len(ctx.Request.Body())), true
}

// selectRoutedModel applies the configured selection strategy. bodySize is the
// request payload size, used by the prompt-size strategy as a stand-in for the
// prompt complexity the real router scores.
func selectRoutedModel(bodySize int) string {
	switch strings.ToLower(strings.TrimSpace(modelRouterStrategy)) {
	case "round-robin":
		next := modelRouterCursor.Add(1) - 1
		return modelRouterCandidates[next%uint64(len(modelRouterCandidates))].model
	case "prompt-size":
		return modelRouterCandidates[promptSizeIndex(bodySize)].model
	default:
		return weightedRouterPick()
	}
}

// weightedRouterPick samples a candidate proportionally to its weight.
func weightedRouterPick() string {
	if modelRouterTotalWeight <= 0 {
		return modelRouterCandidates[0].model
	}
	draw := rand.Intn(modelRouterTotalWeight)
	for _, candidate := range modelRouterCandidates {
		if draw < candidate.weight {
			return candidate.model
		}
		draw -= candidate.weight
	}
	return modelRouterCandidates[len(modelRouterCandidates)-1].model
}

// promptSizeIndex maps a request payload size onto the candidate list. Each
// successive candidate covers a 4x larger payload than the one before it,
// starting at 1KB, so bigger prompts escalate to models listed later — configure
// -model-router-models cheapest first for this strategy.
func promptSizeIndex(bodySize int) int {
	threshold := 1024
	for i := 0; i < len(modelRouterCandidates)-1; i++ {
		if bodySize < threshold {
			return i
		}
		threshold *= 4
	}
	return len(modelRouterCandidates) - 1
}

// parseAzureDeployment extracts the deployment name and the endpoint that
// follows it from an Azure OpenAI-style path, e.g.
// "/openai/deployments/model-router/chat/completions" -> ("model-router", "chat/completions").
// Any prefix before "deployments" is tolerated, so "/azure/openai/deployments/..."
// works too.
func parseAzureDeployment(path string) (deployment string, endpoint string, ok bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for i, segment := range segments {
		if segment != azureDeploymentsSegment || i+2 >= len(segments) {
			continue
		}
		deployment = segments[i+1]
		if unescaped, err := url.PathUnescape(deployment); err == nil {
			deployment = unescaped
		}
		if deployment == "" {
			return "", "", false
		}
		return deployment, strings.Join(segments[i+2:], "/"), true
	}
	return "", "", false
}

// azureDeploymentModel returns the path deployment as a provider-qualified model
// ("azure/<deployment>"), or "" when the request did not come in on an Azure
// deployment route.
func azureDeploymentModel(ctx *fasthttp.RequestCtx) string {
	deployment, ok := ctx.UserValue(azureDeploymentKey).(string)
	if !ok || deployment == "" {
		return ""
	}
	return "azure/" + deployment
}

// handleAzureDeploymentRoute dispatches Azure OpenAI deployment paths. The
// deployment name stands in for the model, since Azure clients address the
// deployment in the URL and omit "model" from the body. It reports whether path
// belonged to one of these routes.
func handleAzureDeploymentRoute(ctx *fasthttp.RequestCtx, path string) bool {
	deployment, endpoint, ok := parseAzureDeployment(path)
	if !ok {
		return false
	}
	switch strings.Trim(endpoint, "/") {
	case "chat/completions":
		ctx.SetUserValue(azureDeploymentKey, deployment)
		mockChatCompletionsHandler(ctx)
	case "responses":
		ctx.SetUserValue(azureDeploymentKey, deployment)
		mockResponsesHandler(ctx)
	case "embeddings":
		ctx.SetUserValue(azureDeploymentKey, deployment)
		mockEmbeddingsHandler(ctx)
	default:
		return false
	}
	return true
}
