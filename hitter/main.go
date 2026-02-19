package main

import (
	"bufio"
	"bytes"
	"context"
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
}

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

	// Random selection
	provider := ""
	if len(config.Providers) > 0 {
		provider = config.Providers[rand.Intn(len(config.Providers))]
	}
	model := config.Models[rand.Intn(len(config.Models))]

	// Random prompt selection
	prompt := prompts[rand.Intn(len(prompts))]

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

	jsonData, err := sonic.Marshal(request)
	if err != nil {
		atomic.AddInt64(&stats.errorRequests, 1)
		if config.Verbose {
			log.Printf("[%d] JSON marshal error: %v", reqNum, err)
		}
		return
	}

	startTime := time.Now()

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", config.URL, bytes.NewBuffer(jsonData))
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
