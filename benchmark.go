// Package main implements a command-line tool for benchmarking API providers.
// It supports configurable request rates, durations, and dynamic payload generation.
// Results, including latency, throughput, and server memory usage, are saved to a JSON file.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/joho/godotenv"
	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/v3/process"
	vegeta "github.com/tsenart/vegeta/v12/lib"

	"bifrost-benchmarks/pkg/concurrent"
)

// Provider represents an API provider to be benchmarked
// It holds the necessary information to target the provider's API.
type Provider struct {
	Name            string // Name of the provider (e.g., "bifrost", "litellm")
	Endpoint        string // API endpoint path (e.g., "v1/chat/completions")
	Port            string // Port number the provider's server is listening on
	Payload         []byte // JSON payload to be used for requests
	PayloadTemplate string // String template for efficient payload generation (pre-built with placeholders)
	RequestType     string // Type of request: "chat" or "embedding"
}

// BenchmarkResult holds the aggregated metrics from a single benchmark run for a provider.
type BenchmarkResult struct {
	ProviderName      string          // Name of the provider benchmarked
	Metrics           *vegeta.Metrics // Vegeta metrics (latency, success rate, etc.)
	CPUUsage          float64         // (Currently unused) Placeholder for CPU usage metrics
	ServerMemoryStats []ServerMemStat // Time-series data of server memory usage during the benchmark
	DropReasons       map[string]int  // Tracks reasons for dropped or failed requests and their counts
}

// MemStat captures generic memory statistics (currently unused in active logic but defined for potential future use).
type MemStat struct {
	Alloc      uint64 // Bytes allocated and still in use
	TotalAlloc uint64 // Bytes allocated (even if freed)
	Sys        uint64 // Bytes obtained from system
	NumGC      uint32 // Number of garbage collections
}

// ServerMemStat captures server memory usage over time
type ServerMemStat struct {
	Timestamp  time.Time
	RSS        uint64  // Resident Set Size in bytes
	VMS        uint64  // Virtual Memory Size in bytes
	MemPercent float64 // Memory usage as percentage
}

// main is the entry point for the benchmarking application.
// It parses command-line flags, initializes the provider, runs the benchmarks,
// and saves the results.
func main() {
	// Define command line flags
	rate := flag.Int("rate", 0, "Requests per second (mutually exclusive with --users)")
	users := flag.Int("users", 0, "Number of concurrent users to maintain (mutually exclusive with --rate)")
	duration := flag.Int("duration", 10, "Duration of test in seconds")
	timeout := flag.Int("timeout", 300, "Request timeout in seconds (should be duration + expected backend latency)")
	outputFile := flag.String("output", "results.json", "Output file for results")
	cooldown := flag.Int("cooldown", 60, "Cooldown period between tests in seconds")
	provider := flag.String("provider", "", "Specific provider to benchmark (bifrost, portkey, braintrust, llmlite, openrouter)")
	bigPayload := flag.Bool("big-payload", false, "Use a bigger payload")
	model := flag.String("model", "gpt-4o-mini", "Model to use")
	suffix := flag.String("suffix", "v1", "Suffix to add to the url route")
	promptFile := flag.String("prompt-file", "", "Path to a file containing the prompt to use")
	path := flag.String("path", "chat/completions", "API path to hit (e.g., 'chat/completions' or 'embeddings')")
	requestType := flag.String("request-type", "chat", "Type of request: 'chat' or 'embedding'")
	host := flag.String("host", "localhost", "Host address for the API server")
	rampUp := flag.Bool("ramp-up", false, "Enable gradual ramp-up of users (only with --users, requires --ramp-up-duration)")
	rampUpDuration := flag.Int("ramp-up-duration", 0, "Duration in seconds to ramp up to target users (only with --users and --ramp-up)")
	debug := flag.Bool("debug", false, "Enable debug mode with detailed logging and periodic status updates")

	// Parse the command line flags.
	flag.Parse()

	// Validate that rate and users are mutually exclusive and at least one is provided
	if *rate > 0 && *users > 0 {
		log.Fatalf("--rate and --users flags are mutually exclusive. Provide only one.")
	}
	if *rate == 0 && *users == 0 {
		log.Fatalf("Either --rate or --users flag must be provided.")
	}

	// Validate ramp-up flags
	if *rampUp || *rampUpDuration > 0 {
		if *users == 0 {
			log.Fatalf("--ramp-up and --ramp-up-duration can only be used with --users flag.")
		}
		if !*rampUp || *rampUpDuration == 0 {
			log.Fatalf("Both --ramp-up and --ramp-up-duration must be provided together.")
		}
		if *rampUpDuration > *duration {
			log.Fatalf("--ramp-up-duration (%d) cannot be greater than --duration (%d).", *rampUpDuration, *duration)
		}
	}

	// Validate request type
	if *requestType != "chat" && *requestType != "embedding" {
		log.Fatalf("Invalid request-type '%s'. Must be 'chat' or 'embedding'", *requestType)
	}

	// Read prompt from file if specified
	var filePrompt string
	if *promptFile != "" {
		promptBytes, err := os.ReadFile(*promptFile)
		if err != nil {
			log.Fatalf("Error reading prompt file '%s': %v", *promptFile, err)
		}
		filePrompt = string(promptBytes)
		fmt.Printf("Loaded prompt from file: %s (%.2f MB)\n", *promptFile, float64(len(filePrompt))/(1024*1024))
	}

	// Initialize providers
	providers := initializeProviders(*bigPayload, *model, *suffix, *path, *requestType, filePrompt, *host)

	// Filter providers if specific provider is requested
	if *provider != "" {
		filteredProviders := make([]Provider, 0)
		for _, p := range providers {
			if strings.EqualFold(p.Name, *provider) {
				filteredProviders = append(filteredProviders, p)
				break
			}
		}
		if len(filteredProviders) == 0 {
			log.Fatalf("Provider '%s' not found. Available providers: %v", *provider, getProviderNames(providers))
		}
		providers = filteredProviders
	} else {
		fmt.Println("No specific provider specified. Running benchmarks for all providers...")
	}

	// Run benchmarks
	results := runBenchmarks(providers, *rate, *users, *duration, *timeout, *cooldown, *rampUp, *rampUpDuration, *debug)

	// Save results
	saveResults(results, *outputFile)
}

// Helper function to get provider names
func getProviderNames(providers []Provider) []string {
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = strings.ToLower(p.Name)
	}
	return names
}

// initializeProvider creates and configures a Provider struct based on the command-line arguments.
// It determines the payload (small or big) and marshals it into JSON bytes.
// Placeholders #{request_index} and #{timestamp} in the payload content will be dynamically replaced.
func initializeProviders(bigPayload bool, model string, suffix string, apiPath string, requestType string, filePrompt string, host string) []Provider {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	// Determine the prompt content
	// #{request_index} is placed at the START to prevent LLM prompt caching
	var promptContent string
	if filePrompt != "" {
		promptContent = "#{request_index} #{timestamp} " + filePrompt
	} else if bigPayload {
		promptContent = "#{request_index} #{timestamp} This is a benchmark request. " +
			"Please provide a comprehensive analysis of the following topics: " +
			"1. Explain the concept of Proxy Gateway in the context of AI, including its architecture, benefits, and use cases. " +
			"2. Discuss the role of load balancing and request routing in AI proxy gateways. " +
			"3. Analyze the impact of caching and rate limiting on AI service performance. " +
			"4. Describe common challenges in implementing AI proxy gateways and potential solutions. " +
			"5. Compare different AI proxy gateway implementations and their trade-offs. " +
			"6. What is the difference between a proxy gateway and a reverse proxy? " +
			"7. What is the difference between a proxy gateway and a load balancer? " +
			"8. What is the difference between a proxy gateway and a web server? " +
			"9. What is the difference between a proxy gateway and a CDN? " +
			"10. What is the difference between a proxy gateway and a firewall? " +
			"11. What is the difference between a proxy gateway and a VPN? " +
			"12. What is the difference between a proxy gateway and a WAF? " +
			"13. What is the difference between a proxy gateway and a DDoS protection service? " +
			"14. What is the difference between a proxy gateway and a DNS server? " +
			"15. What is the difference between a proxy gateway and a web application firewall? " +
			"16. What is the difference between a proxy gateway and a load balancer? " +
			"17. What is the difference between a proxy gateway and a web server? " +
			"18. What is the difference between a proxy gateway and a CDN? " +
			"19. What is the difference between a proxy gateway and a firewall? " +
			"20. What is the difference between a proxy gateway and a VPN? " +
			"Please provide detailed explanations with examples and technical details for each point. "
	} else {
		promptContent = "#{request_index} #{timestamp} This is a benchmark request. How are you?"
	}

	// Create payloads based on request type
	// For Bifrost: use "openai/" prefix
	// For OpenAI: no prefix
	var bifrostPayload []byte
	var openaiPayload []byte

	if requestType == "embedding" {
		// Bifrost embeddings format (with openai/ prefix)
		bifrostPayload, _ = sonic.Marshal(map[string]interface{}{
			"input": promptContent,
			"model": model,
		})
		// OpenAI embeddings format (no prefix)
		openaiPayload, _ = sonic.Marshal(map[string]interface{}{
			"input": promptContent,
			"model": model,
		})
	} else {
		// Bifrost chat completion format (with openai/ prefix)
		bifrostPayload, _ = sonic.Marshal(map[string]interface{}{
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": promptContent,
				},
			},
			"model": model,
		})
		// OpenAI chat completion format (no prefix)
		openaiPayload, _ = sonic.Marshal(map[string]interface{}{
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": promptContent,
				},
			},
			"model": model,
		})
	}

	baseUrl := fmt.Sprintf("http://%s:%%s/%%s/", host) + apiPath
	openaiUrl := fmt.Sprintf("https://api.openai.com/%s", apiPath)

	// Helper function to create payload template from bytes
	createTemplate := func(payloadBytes []byte) string {
		return string(payloadBytes)
	}

	// Create providers - OpenAI and Bifrost for embeddings comparison
	providers := []Provider{
		{
			Name:            "OpenAI",
			Endpoint:        openaiUrl,
			Port:            "", // OpenAI is not localhost, so no port monitoring
			Payload:         openaiPayload,
			PayloadTemplate: createTemplate(openaiPayload),
			RequestType:     requestType,
		},
		{
			Name:            "Bifrost",
			Endpoint:        fmt.Sprintf(baseUrl, os.Getenv("BIFROST_PORT"), suffix),
			Port:            os.Getenv("BIFROST_PORT"),
			Payload:         bifrostPayload,
			PayloadTemplate: createTemplate(bifrostPayload),
			RequestType:     requestType,
		},
		{
			Name:            "Litellm",
			Endpoint:        fmt.Sprintf(baseUrl, os.Getenv("LITELLM_PORT"), suffix),
			Port:            os.Getenv("LITELLM_PORT"),
			Payload:         bifrostPayload, // Use bifrost payload format (with prefix)
			PayloadTemplate: createTemplate(bifrostPayload),
			RequestType:     requestType,
		},
		{
			Name:            "Portkey",
			Endpoint:        fmt.Sprintf(baseUrl, os.Getenv("PORTKEY_PORT"), suffix),
			Port:            os.Getenv("PORTKEY_PORT"),
			Payload:         bifrostPayload, // Use bifrost payload format (with prefix)
			PayloadTemplate: createTemplate(bifrostPayload),
			RequestType:     requestType,
		},
		{
			Name:            "Helicone",
			Endpoint:        fmt.Sprintf(baseUrl, os.Getenv("HELICONE_PORT"), suffix),
			Port:            os.Getenv("HELICONE_PORT"),
			Payload:         bifrostPayload, // Use bifrost payload format (with prefix)
			PayloadTemplate: createTemplate(bifrostPayload),
			RequestType:     requestType,
		},
	}

	return providers
}

func runBenchmarks(providers []Provider, rate int, users int, duration int, timeout int, cooldown int, rampUp bool, rampUpDuration int, debug bool) []BenchmarkResult {
	results := make([]BenchmarkResult, 0, len(providers))

	for i, provider := range providers {
		fmt.Printf("Benchmarking %s...\n", provider.Name)

		httpTransport := &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConnsPerHost: 100000,
			MaxConnsPerHost:     0,
			IdleConnTimeout:     10 * time.Second,
			// Optionally tune TLS and other settings if needed
		}

		httpClient := &http.Client{
			Transport: httpTransport,
			Timeout:   time.Duration(timeout) * time.Second,
		}

		// Define the attack
		targeter := createTargeter(provider)

		// Setup for monitoring server memory usage.
		var serverMemStats []ServerMemStat    // Slice to store memory readings
		var memMutex sync.Mutex               // Mutex to protect concurrent access to serverMemStats
		stopMonitoring := make(chan struct{}) // Channel to signal the monitoring goroutine to stop
		var wg sync.WaitGroup                 // WaitGroup to wait for the monitoring goroutine to finish

		// Initialize drop reasons tracking
		dropReasons := make(map[string]int)

		// Start server memory monitoring (only for localhost providers with a port)
		if provider.Port != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, err := getProcessByPort(provider.Port)
				if err != nil {
					log.Printf("Warning: Could not find process on port %s: %v", provider.Port, err)
					return
				}

				monitorServerMemory(p, stopMonitoring, &serverMemStats, &memMutex)
			}()
		}

		// Create context with timeout for the attack
		ctx, cancel := context.WithTimeout(context.Background(),
			time.Duration(timeout)*time.Second)
		defer cancel()

		// Run the benchmark based on mode
		var metrics vegeta.Metrics

		if users > 0 {
			// Users mode: use concurrent package to maintain N concurrent requests
			runner := concurrent.NewRunner(httpClient, users, time.Duration(duration)*time.Second,
				createConcurrentTargeter(provider), debug)

			// Configure ramp-up if enabled
			if rampUp {
				runner.WithRampUp(time.Duration(rampUpDuration) * time.Second)
			}

			concurrentMetrics := runner.Run(ctx)

			// Convert concurrent metrics to vegeta metrics format
			metrics.Requests = uint64(concurrentMetrics.TotalRequests)
			if concurrentMetrics.TotalRequests > 0 {
				metrics.Success = float64(concurrentMetrics.SuccessCount) / float64(concurrentMetrics.TotalRequests)
			}

			// Calculate latency statistics
			meanLatency := time.Duration(0)
			if concurrentMetrics.TotalRequests > 0 {
				meanLatency = concurrentMetrics.TotalLatency / time.Duration(concurrentMetrics.TotalRequests)
			}
			metrics.Latencies.Mean = meanLatency
			metrics.Latencies.Min = concurrentMetrics.MinLatency
			metrics.Latencies.Max = concurrentMetrics.MaxLatency

			// Count status codes and failures
			statusCodes := make(map[string]int)
			for _, result := range concurrentMetrics.Results {
				if result.Success {
					statusCodes["200"]++
				} else if result.StatusCode > 0 {
					statusCodes[fmt.Sprintf("%d", result.StatusCode)]++
					dropReasons[fmt.Sprintf("HTTP %d", result.StatusCode)]++
				} else {
					dropReasons[result.Error]++
				}
			}
			metrics.StatusCodes = statusCodes

			// Calculate request rate and throughput
			metrics.Rate = float64(concurrentMetrics.TotalRequests) / float64(duration)
			metrics.Throughput = metrics.Rate // Approximate as same as request rate
		} else {
			// Rate mode: use Vegeta with fixed RPS
			attacker := vegeta.NewAttacker(vegeta.Client(httpClient))
			pacer := vegeta.Rate{Freq: rate, Per: time.Second}

			for res := range attacker.Attack(targeter, pacer, time.Duration(duration)*time.Second, provider.Name) {
				metrics.Add(res)

				// Track drop reasons
				if res.Error != "" {
					dropReasons[res.Error]++
				} else if res.Code != 200 {
					dropReasons[fmt.Sprintf("HTTP %d", res.Code)]++
				}

				// Check if context is done
				select {
				case <-ctx.Done():
					log.Printf("Attack for %s timed out", provider.Name)
					dropReasons["context_timeout"]++
					goto EndAttack
				default:
					// Continue with the attack
				}
			}

		EndAttack: // Label to jump to when the attack finishes or times out
			metrics.Close() // Finalize metrics calculation
		}

		// Stop server memory monitoring and wait for it to finish (only if monitoring was started).
		if provider.Port != "" {
			close(stopMonitoring) // Signal the monitorServerMemory goroutine to stop
			wg.Wait()             // Wait for monitorServerMemory to complete
		}

		// Safely copy the collected server memory stats for this benchmark run.
		memMutex.Lock()
		serverMemStatsCopy := make([]ServerMemStat, len(serverMemStats))
		copy(serverMemStatsCopy, serverMemStats)
		memMutex.Unlock()

		// Add results
		results = append(results, BenchmarkResult{
			ProviderName:      provider.Name,
			Metrics:           &metrics,
			ServerMemoryStats: serverMemStatsCopy,
			DropReasons:       dropReasons,
		})

		fmt.Println(metrics.StatusCodes) // Print status code distribution to console

		// Print a summary of the benchmark results to the console.
		fmt.Printf("Results for %s:\n", provider.Name)
		fmt.Printf("  Requests: %d\n", metrics.Requests)
		fmt.Printf("  Request Rate: %.2f/s\n", metrics.Rate)
		fmt.Printf("  Success Rate: %.2f%%\n", 100.0*metrics.Success)
		fmt.Printf("  Mean Latency: %s\n", metrics.Latencies.Mean)
		fmt.Printf("  P50 Latency: %s\n", metrics.Latencies.P50)
		fmt.Printf("  P99 Latency: %s\n", metrics.Latencies.P99)
		fmt.Printf("  Max Latency: %s\n", metrics.Latencies.Max)
		fmt.Printf("  Throughput: %.2f/s\n", metrics.Throughput)

		// Print server memory statistics summary if data was collected.
		if len(serverMemStatsCopy) > 0 {
			var peakMem uint64
			for _, stat := range serverMemStatsCopy {
				if stat.RSS > peakMem {
					peakMem = stat.RSS
				}
			}
			fmt.Printf("  Server Peak Memory: %.2f MB\n\n", float64(peakMem)/(1024*1024))
		} else {
			fmt.Println("  No server memory statistics available")
		}

		// Apply cooldown period between tests (except after the last one)
		if i < len(providers)-1 && cooldown > 0 {
			fmt.Printf("Cooling down for %d seconds...\n", cooldown)
			time.Sleep(time.Duration(cooldown) * time.Second)
		}
	}

	return results
}

// getProcessByPort finds a process listening on the specified TCP port.
// It iterates through system network connections to find a listening process
// matching the given port number and returns a process.Process object for it.
// If no process is found, an error is returned.
func getProcessByPort(port string) (*process.Process, error) {
	portNum, err := strconv.ParseUint(port, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid port number: %v", err)
	}

	// Get all TCP connections on the system.
	conns, err := net.Connections("tcp")
	if err != nil {
		return nil, fmt.Errorf("failed to get connections: %v", err)
	}

	// Iterate through connections to find one listening on the target port.
	for _, conn := range conns {
		if conn.Laddr.Port == uint32(portNum) && conn.Status == "LISTEN" { // Check port and LISTEN status
			// Create a new process object from the PID found.
			p, err := process.NewProcess(conn.Pid)
			if err != nil {
				continue // Skip if process info can't be retrieved
			}
			cmdline, _ := p.Cmdline() // Get command line for logging purposes
			fmt.Printf("Found process on port %s: PID=%d, Cmdline=%s\n", port, conn.Pid, cmdline)
			return p, nil
		}
	}

	return nil, fmt.Errorf("no process found listening on port %s", port)
}

// monitorServerMemory periodically collects memory statistics of the given server process.
// It samples memory usage (RSS, VMS, percent) at 500ms intervals.
// The collected stats are appended to the shared `stats` slice, protected by a mutex.
// Monitoring stops when a signal is received on the `stop` channel.
func monitorServerMemory(p *process.Process, stop <-chan struct{}, stats *[]ServerMemStat, mutex *sync.Mutex) {
	ticker := time.NewTicker(500 * time.Millisecond) // Collect memory stats every 500ms
	defer ticker.Stop()

	for {
		select {
		case <-stop: // If stop signal is received, return and stop monitoring.
			return
		case <-ticker.C: // On every ticker event:
			// Get memory info (RSS, VMS) for the process.
			memInfo, err := p.MemoryInfo()
			if err != nil {
				continue // Skip this tick if there's an error getting memory info
			}

			// Get memory usage percentage for the process.
			memPercent, err := p.MemoryPercent()
			if err != nil {
				memPercent = 0.0 // Default to 0 if there's an error
			}

			// Create a ServerMemStat entry.
			memStat := ServerMemStat{
				Timestamp:  time.Now(),
				RSS:        memInfo.RSS, // Resident Set Size
				VMS:        memInfo.VMS, // Virtual Memory Size
				MemPercent: float64(memPercent),
			}

			// Safely append the new memory stat to the shared slice.
			mutex.Lock()
			*stats = append(*stats, memStat)
			mutex.Unlock()
		}
	}
}

// createTargeter creates a Vegeta Targeter function.
// This function is called by Vegeta for each request it makes.
// It dynamically updates the payload content by replacing placeholders
// `#{request_index}` and `#{timestamp}` with runtime values.
// Uses efficient string templating instead of JSON marshal/unmarshal.
// It also sets up HTTP method, URL, body, and headers for the request.
// Special handling for "portkey" provider includes adding an `x-portkey-config` header.
func createTargeter(provider Provider) vegeta.Targeter {
	// Create a counter for round-robin message selection
	var requestCounter int64
	var counterMutex sync.Mutex

	return func(tgt *vegeta.Target) error {
		// Get next message index in round-robin fashion
		counterMutex.Lock()
		requestCounter++
		counterMutex.Unlock()

		// Use string templating for efficient payload generation
		// Replace placeholders directly in the template string
		updatedPayload := strings.ReplaceAll(provider.PayloadTemplate, "#{request_index}", fmt.Sprintf("%d", requestCounter))
		updatedPayload = strings.ReplaceAll(updatedPayload, "#{timestamp}", time.Now().Format(time.RFC3339))

		// Set up the Vegeta target properties.
		tgt.Method = "POST"
		tgt.URL = provider.Endpoint
		tgt.Body = []byte(updatedPayload)
		tgt.Header = http.Header{
			"Content-Type": []string{"application/json"},
			// "x-bf-vk":      []string{"f452b625-a65e-4dfd-b48d-0ee3ba0e8d46"},
		}

		// Add Authorization header for OpenAI
		if provider.Name == "OpenAI" {
			openaiApiKey := os.Getenv("OPENAI_API_KEY")
			if openaiApiKey == "" {
				return fmt.Errorf("OPENAI_API_KEY is not set")
			}
			tgt.Header.Set("Authorization", fmt.Sprintf("Bearer %s", openaiApiKey))
		}

		if provider.Name == "Portkey" {
			openaiApiKey := os.Getenv("OPENAI_API_KEY")
			if openaiApiKey == "" {
				return fmt.Errorf("OPENAI_API_KEY is not set")
			}
			// Set the x-portkey-config header with OpenAI provider and API key.
			tgt.Header.Set("x-portkey-config", fmt.Sprintf(`{"provider":"openai","api_key":"%s"}`, openaiApiKey))
		}

		return nil
	}
}

// createConcurrentTargeter creates a request generator function for the concurrent package.
// It returns a closure that generates HTTP requests with dynamically updated payloads.
func createConcurrentTargeter(provider Provider) func() (concurrent.Request, error) {
	var requestCounter int64
	var counterMutex sync.Mutex

	return func() (concurrent.Request, error) {
		// Get next message index
		counterMutex.Lock()
		requestCounter++
		counterMutex.Unlock()

		// Use string templating for efficient payload generation
		updatedPayload := strings.ReplaceAll(provider.PayloadTemplate, "#{request_index}", fmt.Sprintf("%d", requestCounter))
		updatedPayload = strings.ReplaceAll(updatedPayload, "#{timestamp}", time.Now().Format(time.RFC3339))

		// Build headers
		headers := http.Header{
			"Content-Type": []string{"application/json"},
		}

		// Add Authorization header for OpenAI
		if provider.Name == "OpenAI" {
			openaiApiKey := os.Getenv("OPENAI_API_KEY")
			if openaiApiKey == "" {
				return concurrent.Request{}, fmt.Errorf("OPENAI_API_KEY is not set")
			}
			headers.Set("Authorization", fmt.Sprintf("Bearer %s", openaiApiKey))
		}

		// Add Portkey config header
		if provider.Name == "Portkey" {
			openaiApiKey := os.Getenv("OPENAI_API_KEY")
			if openaiApiKey == "" {
				return concurrent.Request{}, fmt.Errorf("OPENAI_API_KEY is not set")
			}
			headers.Set("x-portkey-config", fmt.Sprintf(`{"provider":"openai","api_key":"%s"}`, openaiApiKey))
		}

		return concurrent.Request{
			Method:  "POST",
			URL:     provider.Endpoint,
			Headers: headers,
			Body:    []byte(updatedPayload),
		}, nil
	}
}

// saveResults serializes the benchmark results to a JSON file.
// It reads an existing results file if present, updates or adds the new results
// for the current provider (keyed by lowercase provider name), and writes the
// combined results back to the file. Latency values are converted to milliseconds,
// and memory values to megabytes for the output.
func saveResults(results []BenchmarkResult, outputFile string) {
	type SerializableResult struct {
		Requests           uint64         `json:"requests"`
		Rate               float64        `json:"rate"`
		SuccessRate        float64        `json:"success_rate"`
		MeanLatencyMs      float64        `json:"mean_latency_ms"`
		P50LatencyMs       float64        `json:"p50_latency_ms"`
		P99LatencyMs       float64        `json:"p99_latency_ms"`
		MaxLatencyMs       float64        `json:"max_latency_ms"`
		ThroughputRPS      float64        `json:"throughput_rps"`
		Timestamp          string         `json:"timestamp"`
		StatusCodeCounts   map[string]int `json:"status_code_counts"`
		ServerPeakMemoryMB float64        `json:"server_peak_memory_mb"` // Peak server RSS memory during benchmark
		ServerAvgMemoryMB  float64        `json:"server_avg_memory_mb"`  // Average server RSS memory during benchmark
		DropReasons        map[string]int `json:"drop_reasons"`          // Counts of reasons for dropped/failed requests
	}

	// Create a map with provider names as keys
	resultsMap := make(map[string]SerializableResult)

	// Try to read existing results file
	if _, err := os.Stat(outputFile); err == nil {
		fileData, err := os.ReadFile(outputFile)
		if err != nil {
			log.Printf("Warning: Could not read existing results file: %v", err)
		} else {
			if err := sonic.Unmarshal(fileData, &resultsMap); err != nil {
				log.Printf("Warning: Could not parse existing results file: %v", err)
				resultsMap = make(map[string]SerializableResult)
			}
		}
	}

	// Update or add new results
	for _, res := range results {
		// Count status codes
		statusCodes := make(map[string]int)
		for code, count := range res.Metrics.StatusCodes {
			statusCodes[code] = int(count)
		}

		// Calculate peak and average server memory if available
		var peakMem uint64
		var totalMem uint64
		for _, stat := range res.ServerMemoryStats {
			if stat.RSS > peakMem {
				peakMem = stat.RSS
			}
			totalMem += stat.RSS
		}

		var avgMem float64
		if len(res.ServerMemoryStats) > 0 {
			avgMem = float64(totalMem) / float64(len(res.ServerMemoryStats)) / (1024 * 1024)
		}

		resultsMap[strings.ToLower(res.ProviderName)] = SerializableResult{
			Requests:           res.Metrics.Requests,
			Rate:               res.Metrics.Rate,
			SuccessRate:        100.0 * res.Metrics.Success,
			MeanLatencyMs:      float64(res.Metrics.Latencies.Mean) / float64(time.Millisecond),
			P50LatencyMs:       float64(res.Metrics.Latencies.P50) / float64(time.Millisecond),
			P99LatencyMs:       float64(res.Metrics.Latencies.P99) / float64(time.Millisecond),
			MaxLatencyMs:       float64(res.Metrics.Latencies.Max) / float64(time.Millisecond),
			ThroughputRPS:      res.Metrics.Throughput,
			Timestamp:          time.Now().Format(time.RFC3339),
			StatusCodeCounts:   statusCodes,
			ServerPeakMemoryMB: float64(peakMem) / (1024 * 1024),
			ServerAvgMemoryMB:  avgMem,
			DropReasons:        res.DropReasons,
		}
	}

	// Marshal the updated resultsMap to JSON with indentation.
	jsonData, err := sonic.MarshalIndent(resultsMap, "", "  ")
	if err != nil {
		log.Fatalf("Error marshaling results: %v", err)
	}

	// Write the JSON data to the output file.
	err = os.WriteFile(outputFile, jsonData, 0644)
	if err != nil {
		log.Fatalf("Error writing results to file: %v", err)
	}

	fmt.Printf("Results saved to %s\n", outputFile)
}
