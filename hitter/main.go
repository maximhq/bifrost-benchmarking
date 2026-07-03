package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bytedance/sonic"
)

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Multimodal request shapes used when an attachment (e.g. --pdf) is supplied.
// Content becomes an array of typed parts instead of a plain string.
type MultiModalRequest struct {
	Model       string              `json:"model"`
	Messages    []MultiModalMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
}

type MultiModalMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

type ContentPart struct {
	Type string    `json:"type"`
	Text string    `json:"text,omitempty"`
	File *FilePart `json:"file,omitempty"`
}

// FilePart mirrors Bifrost's OpenAI "file" content block (ChatInputFile).
type FilePart struct {
	FileData string `json:"file_data"`
	Filename string `json:"filename,omitempty"`
}

type Config struct {
	URL         string
	RPS         int
	Duration    time.Duration
	Models      []string
	Providers   []string
	MaxTokens   int
	Temperature float64
	Verbose     bool
	Stream      bool
	VirtualKey  string
	PDFPath     string
	Prompt      string
}

// Prebuilt request bodies, populated once at startup when --pdf is set so the
// large (~27MB base64) body is encoded a single time and reused for every
// request — keeps the hitter from becoming CPU-bound on JSON marshaling.
var (
	prebuiltBodies [][]byte
	prebuiltLabels []string
)

type Stats struct {
	totalRequests   int64
	successRequests int64
	errorRequests   int64
}

var prompts = []string{
	"Explain quantum computing in simple terms.",
	"Write a short story about a robot learning to paint.",
	"What are the benefits of renewable energy?",
	"Describe the process of photosynthesis.",
	"How does machine learning work?",
	"Write a poem about the ocean.",
	"Explain the theory of relativity.",
	"What is the importance of biodiversity?",
	"Describe how blockchain technology works.",
	"Write a recipe for chocolate chip cookies.",
	"What are the causes of climate change?",
	"Explain how neural networks function.",
	"Describe the water cycle process.",
	"What is artificial intelligence?",
	"Write a brief history of the internet.",
	"How do vaccines work?",
	"What is sustainable development?",
	"Explain the concept of entropy.",
	"Describe how GPS systems work.",
	"What are the phases of the moon?",
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func main() {
	config := parseFlags()

	log.Printf("🚀 Starting Load Test")
	log.Printf("   URL: %s", config.URL)
	log.Printf("   RPS: %d", config.RPS)
	log.Printf("   Duration: %s", config.Duration)
	log.Printf("   Models: %v", config.Models)
	log.Printf("   Providers: %v", config.Providers)
	log.Printf("   Stream: %v", config.Stream)

	// Attachment mode: pre-encode the PDF into reusable request bodies.
	if config.PDFPath != "" {
		buildPDFBodies(config)
	}

	stats := &Stats{}

	// Setup signal handling
	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n📊 Stopping load test...")
		cancel()
	}()

	// Start load test
	startTime := time.Now()
	endTime := startTime.Add(config.Duration)

	// Rate limiter
	ticker := time.NewTicker(time.Second / time.Duration(config.RPS))
	defer ticker.Stop()

	// Basic stats printer every 10 seconds
	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	var wg sync.WaitGroup

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-statsTicker.C:
				printBasicStats(stats, time.Since(startTime))
			}
		}
	}()

	requestCount := 0
	for {
		select {
		case <-ctx.Done():
			goto cleanup
		case <-ticker.C:
			if time.Now().After(endTime) {
				goto cleanup
			}

			wg.Add(1)
			go func(reqNum int) {
				defer wg.Done()
				makeRequest(ctx, config, stats, reqNum)
			}(requestCount)
			requestCount++
		}
	}

cleanup:
	log.Println("⏳ Waiting for remaining requests to complete...")
	wg.Wait()

	totalDuration := time.Since(startTime)
	log.Printf("\n✅ Load test completed in %s", totalDuration)
	printFinalStats(stats, totalDuration)
}

func parseFlags() *Config {
	config := &Config{}

	flag.StringVar(&config.URL, "url", "http://localhost:8080/v1/chat/completions", "Target URL")
	flag.IntVar(&config.RPS, "rps", 100, "Requests per second")
	flag.DurationVar(&config.Duration, "duration", 60*time.Second, "Test duration")
	flag.IntVar(&config.MaxTokens, "max-tokens", 150, "Max tokens per request")
	flag.Float64Var(&config.Temperature, "temperature", 0.7, "Temperature for requests")
	flag.BoolVar(&config.Verbose, "verbose", false, "Verbose logging")
	flag.BoolVar(&config.Stream, "stream", false, "Enable streaming responses")
	flag.StringVar(&config.VirtualKey, "virtual-key", "", "Virtual key to use for requests")
	flag.StringVar(&config.PDFPath, "pdf", "", "Path to a PDF file to attach as a multimodal 'file' content block (enables attachment mode)")
	flag.StringVar(&config.Prompt, "prompt", "", "Override the user prompt text (defaults to a random prompt, or a fixed summarize prompt in --pdf mode)")

	modelsFlag := flag.String("models", "gpt-4,gpt-4o,gpt-4o-mini,gpt-4.1,gpt-5", "Comma-separated list of models")
	providersFlag := flag.String("providers", "", "Comma-separated list of providers")

	flag.Parse()

	// Parse models and providers
	if *modelsFlag != "" {
		config.Models = parseCommaSeparated(*modelsFlag)
	}
	if *providersFlag != "" {
		config.Providers = parseCommaSeparated(*providersFlag)
	}

	// Validation
	if config.RPS <= 0 {
		log.Fatal("RPS must be greater than 0")
	}
	if config.Duration <= 0 {
		log.Fatal("Duration must be greater than 0")
	}
	if len(config.Models) == 0 {
		config.Models = []string{"gpt-4", "gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-5"}
	}
	if len(config.Providers) == 0 {
		log.Println("At least one provider must be specified, sending request without provider")
	}

	return config
}

// buildPDFBodies reads the PDF once, base64-encodes it once, and pre-marshals
// one request body per model×provider combination. The bodies are reused for
// every request so the large attachment is never re-encoded at request time.
func buildPDFBodies(config *Config) {
	data, err := os.ReadFile(config.PDFPath)
	if err != nil {
		log.Fatalf("Failed to read PDF %q: %v", config.PDFPath, err)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	dataURL := "data:application/pdf;base64," + b64
	log.Printf("📎 Loaded PDF %s: %d bytes raw, %d bytes base64", config.PDFPath, len(data), len(b64))

	prompt := config.Prompt
	if prompt == "" {
		prompt = "Summarize the attached PDF document in detail."
	}

	providers := config.Providers
	if len(providers) == 0 {
		providers = []string{""}
	}

	for _, p := range providers {
		for _, m := range config.Models {
			model := m
			if p != "" {
				model = p + "/" + m
			}
			req := MultiModalRequest{
				Model: model,
				Messages: []MultiModalMessage{
					{
						Role: "user",
						Content: []ContentPart{
							{Type: "text", Text: prompt},
							{Type: "file", File: &FilePart{FileData: dataURL, Filename: "test.pdf"}},
						},
					},
				},
				MaxTokens:   config.MaxTokens,
				Temperature: config.Temperature,
				Stream:      config.Stream,
			}
			body, err := sonic.Marshal(req)
			if err != nil {
				log.Fatalf("Failed to marshal PDF request for %q: %v", model, err)
			}
			prebuiltBodies = append(prebuiltBodies, body)
			prebuiltLabels = append(prebuiltLabels, model)
		}
	}
	log.Printf("📦 Prebuilt %d PDF request body/bodies, ~%d MB each", len(prebuiltBodies), len(prebuiltBodies[0])/(1024*1024))
}

func parseCommaSeparated(s string) []string {
	var result []string
	for _, segment := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(segment)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func makeRequest(ctx context.Context, config *Config, stats *Stats, reqNum int) {
	atomic.AddInt64(&stats.totalRequests, 1)

	var jsonData []byte
	var model string
	provider := ""

	if len(prebuiltBodies) > 0 {
		// Attachment mode: reuse a pre-encoded body (no per-request marshaling).
		idx := rand.Intn(len(prebuiltBodies))
		jsonData = prebuiltBodies[idx]
		model = prebuiltLabels[idx]
	} else {
		// Random selection
		if len(config.Providers) > 0 {
			provider = config.Providers[rand.Intn(len(config.Providers))]
		}
		model = config.Models[rand.Intn(len(config.Models))]

		// Random prompt selection
		prompt := prompts[rand.Intn(len(prompts))]
		if config.Prompt != "" {
			prompt = config.Prompt
		}

		// Add some variation to token usage
		maxTokens := config.MaxTokens + rand.Intn(50) - 25 // ±25 tokens variation
		if maxTokens < 10 {
			maxTokens = 10
		}

		if provider != "" {
			model = provider + "/" + model
		}

		request := ChatRequest{
			Model: model,
			Messages: []Message{
				{
					Role:    "user",
					Content: prompt,
				},
			},
			MaxTokens:   maxTokens,
			Temperature: config.Temperature + (rand.Float64()-0.5)*0.2, // ±0.1 variation
			Stream:      config.Stream,
		}

		var err error
		jsonData, err = sonic.Marshal(request)
		if err != nil {
			atomic.AddInt64(&stats.errorRequests, 1)
			if config.Verbose {
				log.Printf("[%d] JSON marshal error: %v", reqNum, err)
			}
			return
		}
	}

	startTime := time.Now()

	// Create HTTP request (bytes.NewReader shares the prebuilt slice without copying)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", config.URL, bytes.NewReader(jsonData))
	if err != nil {
		atomic.AddInt64(&stats.errorRequests, 1)
		if config.Verbose {
			log.Printf("[%d] Request creation error: %v", reqNum, err)
		}
		return
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	if config.VirtualKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+config.VirtualKey)
	}

	// Make request
	resp, err := httpClient.Do(httpReq)
	latency := time.Since(startTime)

	if err != nil {
		atomic.AddInt64(&stats.errorRequests, 1)
		if config.Verbose {
			log.Printf("[%d] HTTP request error: %v", reqNum, err)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		// If streaming, read the stream to completion
		if config.Stream {
			if err := readStream(resp.Body, config.Verbose, reqNum); err != nil {
				atomic.AddInt64(&stats.errorRequests, 1)
				if config.Verbose {
					log.Printf("[%d] Stream read error: %v", reqNum, err)
				}
				return
			}
		} else {
			// For non-streaming, just read the body to completion
			_, err := io.ReadAll(resp.Body)
			if err != nil {
				atomic.AddInt64(&stats.errorRequests, 1)
				if config.Verbose {
					log.Printf("[%d] Response read error: %v", reqNum, err)
				}
				return
			}
		}
		atomic.AddInt64(&stats.successRequests, 1)
	} else {
		atomic.AddInt64(&stats.errorRequests, 1)
	}

	// Log verbose output
	if config.Verbose {
		log.Printf("[%d] %s (%s) -> %d in %dms",
			reqNum, model, provider, resp.StatusCode, latency.Milliseconds())
	}
}

func printBasicStats(stats *Stats, elapsed time.Duration) {
	total := atomic.LoadInt64(&stats.totalRequests)
	success := atomic.LoadInt64(&stats.successRequests)

	var successRate float64
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	currentRPS := float64(total) / elapsed.Seconds()

	log.Printf("📈 [%s] Requests: %d | Success: %.1f%% | RPS: %.1f",
		elapsed.Truncate(time.Second), total, successRate, currentRPS)
}

func readStream(body io.Reader, verbose bool, reqNum int) error {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			if verbose {
				// Optionally log stream chunks (can be verbose)
				_ = data
			}
		}
	}
	return scanner.Err()
}

func printFinalStats(stats *Stats, duration time.Duration) {
	total := atomic.LoadInt64(&stats.totalRequests)
	success := atomic.LoadInt64(&stats.successRequests)
	errors := atomic.LoadInt64(&stats.errorRequests)

	var successRate float64
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	avgRPS := float64(total) / duration.Seconds()

	log.Printf("\n📋 FINAL STATISTICS")
	log.Printf("   Duration: %s", duration.Truncate(time.Millisecond))
	log.Printf("   Total Requests: %d", total)
	log.Printf("   Successful: %d (%.1f%%)", success, successRate)
	log.Printf("   Errors: %d", errors)
	log.Printf("   Average RPS: %.1f", avgRPS)
}
