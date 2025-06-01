// Package main implements a command-line tool for benchmarking API providers.
// It supports configurable request rates, durations, and dynamic payload generation.
// Results, including latency, throughput, and server memory usage, are saved to a JSON file.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/net"
	"github.com/shirou/gopsutil/v3/process"
	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// Provider represents an API provider to be benchmarked
// It holds the necessary information to target the provider's API.
type Provider struct {
	Name     string // Name of the provider (e.g., "bifrost", "litellm")
	Endpoint string // API endpoint path (e.g., "v1/chat/completions")
	Port     string // Port number the provider's server is listening on
	Payload  []byte // JSON payload to be used for requests
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

// ServerMemStat captures server memory usage at a specific point in time.
type ServerMemStat struct {
	Timestamp  time.Time // Timestamp of the memory measurement
	RSS        uint64    // Resident Set Size in bytes (actual physical memory used)
	VMS        uint64    // Virtual Memory Size in bytes
	MemPercent float64   // Memory usage as a percentage of total system memory
}

// main is the entry point for the benchmarking application.
// It parses command-line flags, initializes the provider, runs the benchmarks,
// and saves the results.
func main() {
	// Define command line flags to configure the benchmark parameters.
	rate := flag.Int("rate", 500, "Requests per second")
	duration := flag.Int("duration", 10, "Duration of test in seconds")
	outputFile := flag.String("output", "results.json", "Output file for results")
	providerName := flag.String("provider", "", "Specific provider to benchmark (bifrost, portkey, braintrust, litellm, openrouter, etc)")
	port := flag.String("port", "", "Port for the provider")
	endpoint := flag.String("endpoint", "v1/chat/completions", "Endpoint for the provider")
	includeProviderInRequest := flag.Bool("include-provider-in-request", false, "Include the provider name in the request")
	bigPayload := flag.Bool("big-payload", false, "Use a bigger payload")

	// Parse the command line flags.
	flag.Parse()

	// Validate required flags.
	if *port == "" {
		log.Fatalf("Port is required")
	}

	if *providerName == "" {
		log.Fatalf("Provider is required")
	}

	// Initialize the provider configuration based on flags.
	provider := initializeProvider(*bigPayload, *port, *endpoint, *providerName, *includeProviderInRequest)

	// Run the benchmark for the configured provider.
	results := runBenchmarks(provider, *rate, *duration)

	// Save the benchmark results to the specified output file.
	saveResults(results, *outputFile)
}

// initializeProvider creates and configures a Provider struct based on the command-line arguments.
// It determines the payload (small or big) and marshals it into JSON bytes.
// Placeholders #{request_index} and #{timestamp} in the payload content will be dynamically replaced.
func initializeProvider(bigPayload bool, port string, endpoint string, providerName string, includeProviderInRequest bool) Provider {
	var payload map[string]interface{}

	if bigPayload {
		// Use a larger, more complex payload if bigPayload flag is true.
		payload = map[string]interface{}{
			"messages": []map[string]string{
				{
					"role": "user",
					"content": "This is a benchmark request #{request_index} at #{timestamp}. " +
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
						"Please provide detailed explanations with examples and technical details for each point. ",
				},
			},
			"model": "gpt-4o-mini",
		}
	} else {
		// Use a smaller, simpler payload by default.
		payload = map[string]interface{}{
			"messages": []map[string]string{
				{
					"role":    "user",
					"content": "This is a benchmark request #{request_index} at #{timestamp}. How are you?",
				},
			},
			"model": "gpt-4o-mini",
		}
	}

	// Optionally include the provider name in the request payload.
	if includeProviderInRequest {
		payload["provider"] = providerName
	}

	// Marshal the payload into JSON bytes.
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Error marshalling payload: %v", err)
	}

	return Provider{
		Name:     providerName,
		Endpoint: endpoint,
		Port:     port,
		Payload:  payloadBytes,
	}
}

// runBenchmarks executes the Vegeta attack against the specified provider.
// It configures an HTTP client, sets up Vegeta, monitors server memory,
// collects metrics, and prints a summary to the console.
func runBenchmarks(provider Provider, rate int, duration int) []BenchmarkResult {
	results := make([]BenchmarkResult, 0, 1) // Initialize a slice to hold results (currently for one provider at a time)

	fmt.Printf("Benchmarking %s...\n", provider.Name)

	// Configure a custom HTTP transport for Vegeta to allow high concurrency.
	httpTransport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment, // Respect proxy environment variables
		MaxIdleConnsPerHost: 100000,                    // High number of idle connections to keep alive
		MaxConnsPerHost:     0,                         // No limit on total connections per host
		IdleConnTimeout:     10 * time.Second,          // Timeout for idle connections
	}

	// Create an HTTP client with the custom transport and a request timeout.
	httpClient := &http.Client{
		Transport: httpTransport,
		Timeout:   240 * time.Second, // Overall timeout for each request
	}

	// Create a Vegeta targeter, which generates requests dynamically.
	targeter := createTargeter(provider)
	// Create a Vegeta attacker using the custom HTTP client.
	attacker := vegeta.NewAttacker(vegeta.Client(httpClient))

	// Setup for monitoring server memory usage.
	var serverMemStats []ServerMemStat    // Slice to store memory readings
	var memMutex sync.Mutex               // Mutex to protect concurrent access to serverMemStats
	stopMonitoring := make(chan struct{}) // Channel to signal the monitoring goroutine to stop
	var wg sync.WaitGroup                 // WaitGroup to wait for the monitoring goroutine to finish

	// Initialize map to track reasons for dropped/failed requests.
	dropReasons := make(map[string]int)

	// Start server memory monitoring in a separate goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Find the process ID of the server running on the specified port.
		p, err := getProcessByPort(provider.Port)
		if err != nil {
			// Log a warning if the process can't be found, but continue the benchmark.
			log.Printf("Warning: Could not find process on port %s: %v", provider.Port, err)
			return
		}

		// Start monitoring the identified process's memory.
		monitorServerMemory(p, stopMonitoring, &serverMemStats, &memMutex)
	}()

	// Create a context with a timeout for the entire attack duration to prevent hangs.
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Duration(240)*time.Second) // Overall timeout for the attack itself
	defer cancel() // Ensure cancel is called to release resources

	// Run the Vegeta attack and collect metrics.
	var metrics vegeta.Metrics
	attackRate := vegeta.Rate{Freq: rate, Per: time.Second} // Define the request rate
	// Iterate over the results channel from the attacker.
	for res := range attacker.Attack(targeter, attackRate, time.Duration(duration)*time.Second, provider.Name) {
		metrics.Add(res) // Add each result to the Vegeta metrics accumulator

		// Track reasons for dropped or non-200 responses.
		if res.Error != "" {
			dropReasons[res.Error]++
		} else if res.Code != 200 {
			dropReasons[fmt.Sprintf("HTTP %d", res.Code)]++
		}

		// Check if the overall attack context has been cancelled (e.g., due to timeout).
		select {
		case <-ctx.Done():
			log.Printf("Attack for %s timed out", provider.Name)
			dropReasons["context_timeout"]++
			goto EndAttack // Exit the loop if the attack timed out
		default:
			// Continue with the attack if context is not done.
		}
	}

EndAttack: // Label to jump to when the attack finishes or times out
	metrics.Close() // Finalize metrics calculation

	// Stop server memory monitoring and wait for it to finish.
	close(stopMonitoring) // Signal the monitorServerMemory goroutine to stop
	wg.Wait()             // Wait for monitorServerMemory to complete

	// Safely copy the collected server memory stats for this benchmark run.
	memMutex.Lock()
	serverMemStatsCopy := make([]ServerMemStat, len(serverMemStats))
	copy(serverMemStatsCopy, serverMemStats)
	memMutex.Unlock()

	// Append the results of this benchmark run.
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
// It samples memory usage (RSS, VMS, percent) at 100ms intervals.
// The collected stats are appended to the shared `stats` slice, protected by a mutex.
// Monitoring stops when a signal is received on the `stop` channel.
func monitorServerMemory(p *process.Process, stop <-chan struct{}, stats *[]ServerMemStat, mutex *sync.Mutex) {
	ticker := time.NewTicker(100 * time.Millisecond) // Collect memory stats every 100ms
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
// It also sets up HTTP method, URL, body, and headers for the request.
// Special handling for "portkey" provider includes adding an `x-portkey-config` header.
func createTargeter(provider Provider) vegeta.Targeter {
	var requestCounter int64                       // Counter to ensure unique request_index for each request
	var counterMutex sync.Mutex                    // Mutex to protect concurrent access to requestCounter
	providerName := strings.ToLower(provider.Name) // Lowercase provider name for specific logic (e.g., Portkey headers)

	return func(tgt *vegeta.Target) error {
		// Increment request counter atomically.
		counterMutex.Lock()
		requestCounter++
		counterMutex.Unlock()

		// Unmarshal the base payload provided for the provider.
		var payload map[string]interface{}
		if err := json.Unmarshal(provider.Payload, &payload); err != nil {
			return err
		}

		// Assuming the payload structure has "messages"[0]["content"] as a string.
		// This might need to be made more robust if payload structures vary significantly.
		text := payload["messages"].([]interface{})[0].(map[string]interface{})["content"].(string)

		// Replace placeholders in the content string.
		updatedText := strings.ReplaceAll(text, "#{request_index}", fmt.Sprintf("%d", requestCounter))
		updatedText = strings.ReplaceAll(updatedText, "#{timestamp}", time.Now().Format(time.RFC3339))

		payload["messages"].([]interface{})[0].(map[string]interface{})["content"] = updatedText

		// Marshal the updated payload back to JSON.
		updatedPayload, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		// Set up the Vegeta target properties.
		tgt.Method = "POST"
		// Construct the full URL using localhost, provider's port, and endpoint.
		tgt.URL = fmt.Sprintf("http://localhost:%s/%s", provider.Port, provider.Endpoint)
		tgt.Body = updatedPayload
		tgt.Header = http.Header{
			"Content-Type": []string{"application/json"},
		}

		// Add specific headers if the provider is Portkey.
		if providerName == "portkey" {
			openaiApiKey := os.Getenv("OPENAI_API_KEY")
			if openaiApiKey == "" {
				return fmt.Errorf("OPENAI_API_KEY is not set for Portkey targeter")
			}
			// Set the x-portkey-config header with OpenAI provider and API key.
			tgt.Header.Set("x-portkey-config", fmt.Sprintf(`{"provider":"openai","api_key":"%s"}`, openaiApiKey))
		}

		return nil // Return nil if target setup is successful
	}
}

// saveResults serializes the benchmark results to a JSON file.
// It reads an existing results file if present, updates or adds the new results
// for the current provider (keyed by lowercase provider name), and writes the
// combined results back to the file. Latency values are converted to milliseconds,
// and memory values to megabytes for the output.
func saveResults(results []BenchmarkResult, outputFile string) {
	// SerializableResult defines the structure for JSON output, converting durations to milliseconds.
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

	// resultsMap holds results from potentially multiple providers, keyed by provider name.
	resultsMap := make(map[string]SerializableResult)

	// Try to read and unmarshal an existing results file to append/update.
	if _, err := os.Stat(outputFile); err == nil { // Check if file exists
		fileData, err := os.ReadFile(outputFile)
		if err != nil {
			log.Printf("Warning: Could not read existing results file: %v", err)
		} else {
			if err := json.Unmarshal(fileData, &resultsMap); err != nil {
				log.Printf("Warning: Could not parse existing results file: %v", err)
				resultsMap = make(map[string]SerializableResult) // Reset if parsing fails
			}
		}
	}

	// Process each BenchmarkResult and add it to the resultsMap.
	for _, res := range results {
		// Convert Vegeta's status code map to a simpler map[string]int.
		statusCodes := make(map[string]int)
		for code, count := range res.Metrics.StatusCodes {
			statusCodes[code] = int(count)
		}

		// Calculate peak, average, before, and after server memory usage in MB.
		var peakMem uint64
		var totalMem uint64

		if len(res.ServerMemoryStats) > 0 {
			for _, stat := range res.ServerMemoryStats {
				if stat.RSS > peakMem {
					peakMem = stat.RSS
				}
				totalMem += stat.RSS
			}
		}

		var avgMem float64
		if len(res.ServerMemoryStats) > 0 {
			avgMem = float64(totalMem) / float64(len(res.ServerMemoryStats)) / (1024 * 1024)
		}

		// Populate the SerializableResult for the current provider.
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
	jsonData, err := json.MarshalIndent(resultsMap, "", "  ")
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
