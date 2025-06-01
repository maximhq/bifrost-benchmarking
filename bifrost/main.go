// Package main implements the Bifrost API gateway server.
// It initializes and runs an HTTP server that proxies requests to backend services (e.g., OpenAI)
// using the Bifrost core library. The server supports configurable port, proxy settings,
// concurrency, buffer sizes, and a debug mode for metrics and detailed request/response logging.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/fasthttp/router"
	"github.com/maximhq/bifrost-gateway/lib"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/valyala/fasthttp"
)

// Command-line flags
var (
	port     string // port defines the port on which the server will listen.
	proxyURL string // proxyURL allows specifying an HTTP proxy for outgoing requests.
	debug    bool   // debug enables or disables debug mode, which includes metrics and detailed logging.

	// Bifrost client tuning parameters
	concurrency     int // concurrency sets the maximum number of concurrent requests for the Bifrost client.
	bufferSize      int // bufferSize defines the buffer size for the Bifrost client.
	initialPoolSize int // initialPoolSize sets the initial size of Bifrost's internal object pools.
)

// init parses command-line flags and loads the OpenAI API key.
// If the openai-key flag is not provided, it attempts to load it from a .env file
// in the parent directory.
func init() {
	flag.StringVar(&port, "port", "3001", "Port to run the server on")
	flag.StringVar(&proxyURL, "proxy", "", "Proxy URL (e.g., http://localhost:8080)")
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")

	flag.IntVar(&concurrency, "concurrency", 20000, "Concurrency level for Bifrost client")
	flag.IntVar(&bufferSize, "buffer-size", 25000, "Buffer size for Bifrost client")
	flag.IntVar(&initialPoolSize, "initial-pool-size", 25000, "Initial pool size for Bifrost client objects")

	flag.Parse()

	// OpenAI key is mandatory for the server to function correctly with OpenAI provider.
	if os.Getenv("OPENAI_API_KEY") == "" {
		log.Fatalf("OpenAI API key is required. Please set the OPENAI_API_KEY environment variable.")
	}
}

// ChatRequest defines the expected structure for incoming chat completion requests.
// It mirrors a subset of typical OpenAI chat completion request fields.
type ChatRequest struct {
	Messages []schemas.Message `json:"messages"` // A list of messages comprising the conversation so far.
	Model    string            `json:"model"`    // ID of the model to use.
}

// main is the entry point of the Bifrost gateway server.
// It initializes the server, sets up routing, configures Bifrost client,
// and handles graceful shutdown.
func main() {
	// Set GOMAXPROCS to utilize all available CPU cores for optimal performance.
	runtime.GOMAXPROCS(runtime.NumCPU())

	// Initialize the Bifrost account configuration.
	account := lib.NewBaseAccount(os.Getenv("OPENAI_API_KEY"), proxyURL, concurrency, bufferSize)

	// Initialize the Bifrost client with the account, no plugins, default logger, and initial pool size.
	client, err := bifrost.Init(schemas.BifrostConfig{
		Account:         account,
		Plugins:         []schemas.Plugin{},
		Logger:          nil, // Using default logger
		InitialPoolSize: initialPoolSize,
	})
	if err != nil {
		log.Fatalf("Failed to initialize Bifrost: %v", err)
	}

	// Create a new fasthttp router.
	r := router.New()

	// Setup handlers based on whether debug mode is enabled.
	if debug {
		// In debug mode, use the DebugHandler for chat completions to get detailed logs.
		r.POST("/v1/chat/completions", lib.DebugHandler(client))
		// Expose a /metrics endpoint for Prometheus or general stats in debug mode.
		r.GET("/metrics", lib.GetMetricsHandler())
	} else {
		// Standard handler for chat completions in non-debug (production) mode.
		Handler := func(ctx *fasthttp.RequestCtx) {
			var chatReq ChatRequest
			// Unmarshal the request body into ChatRequest struct.
			if err := json.Unmarshal(ctx.PostBody(), &chatReq); err != nil {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBodyString(fmt.Sprintf("invalid request format: %v", err))
				return
			}

			// Transform the incoming ChatRequest into a BifrostRequest.
			bifrostReq := &schemas.BifrostRequest{
				Provider: schemas.OpenAI, // Assuming OpenAI provider for this handler
				Model:    chatReq.Model,
				Input: schemas.RequestInput{
					ChatCompletionInput: &chatReq.Messages,
				},
			}

			// Validate that messages are provided.
			if len(chatReq.Messages) == 0 {
				ctx.SetStatusCode(fasthttp.StatusBadRequest)
				ctx.SetBodyString("Messages array is required")
				return
			}

			// Perform the chat completion request using the Bifrost client.
			resp, err := client.ChatCompletionRequest(ctx, bifrostReq)
			if err != nil {
				ctx.SetStatusCode(fasthttp.StatusInternalServerError)
				ctx.SetBodyString(fmt.Sprintf("error processing chat completion: %v", err))
				return
			}

			// Send the successful response back to the client.
			ctx.SetStatusCode(fasthttp.StatusOK)
			ctx.SetContentType("application/json")
			json.NewEncoder(ctx).Encode(resp)
		}

		// Register the standard handler for the chat completions endpoint.
		r.POST("/v1/chat/completions", Handler)
	}

	// Configure the fasthttp server with the router and performance settings.
	server := &fasthttp.Server{
		Handler:               r.Handler, // Use the configured router
		NoDefaultServerHeader: true,      // Do not send the default "Server: fasthttp" header
		TCPKeepalive:          true,      // Enable TCP keep-alive
		Concurrency:           0,         // Unlimited concurrent connections (fasthttp manages this internally)
	}

	// Set up a channel to listen for OS signals for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM) // Listen for Interrupt and Terminate signals

	// Start the fasthttp server in a separate goroutine.
	go func() {
		fmt.Printf("Bifrost API server starting on port %s...\n", port)
		if err := server.ListenAndServe(":" + port); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Block until a shutdown signal is received.
	<-sigChan
	fmt.Println("\nShutting down server...")

	// Perform Bifrost client cleanup.
	client.Shutdown()

	// Attempt to gracefully shut down the server.
	if err := server.Shutdown(); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	// If in debug mode, print collected statistics.
	if debug {
		lib.PrintStats()
	}

	fmt.Println("Server gracefully stopped.")
}
