// Package concurrent provides concurrent request execution with semaphore-based concurrency control.
// It maintains a fixed number of concurrent requests in flight and tracks success rates.
package concurrent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Request represents a single HTTP request to be made.
type Request struct {
	Method  string
	URL     string
	Headers http.Header
	Body    []byte
}

// Result represents the outcome of a single request.
type Result struct {
	StatusCode int
	Latency    time.Duration
	Error      string
	Success    bool
}

// Metrics holds aggregated metrics from a concurrent benchmark run.
type Metrics struct {
	TotalRequests  int
	SuccessCount   int
	FailureCount   int
	SuccessRate    float64
	Results        []Result
	TotalLatency    time.Duration
	MinLatency      time.Duration
	MaxLatency      time.Duration
	mu             sync.Mutex
}

// Runner executes requests concurrently while maintaining a fixed number of in-flight requests.
type Runner struct {
	client         *http.Client
	numUsers       int
	duration       time.Duration
	requestGen     func() (Request, error)
	metrics        *Metrics
	semaphore      chan struct{}
	wg             sync.WaitGroup
	rampUp         bool
	rampUpDuration time.Duration
}

// NewRunner creates a new concurrent request runner.
func NewRunner(client *http.Client, numUsers int, duration time.Duration, requestGen func() (Request, error)) *Runner {
	return &Runner{
		client:     client,
		numUsers:   numUsers,
		duration:   duration,
		requestGen: requestGen,
		metrics: &Metrics{
			Results: make([]Result, 0),
		},
		semaphore: make(chan struct{}, numUsers),
	}
}

// WithRampUp configures ramp-up behavior for the runner.
func (r *Runner) WithRampUp(rampUpDuration time.Duration) *Runner {
	r.rampUp = true
	r.rampUpDuration = rampUpDuration
	return r
}

// Run executes the concurrent request benchmark and returns metrics.
func (r *Runner) Run(ctx context.Context) *Metrics {
	ctx, cancel := context.WithTimeout(ctx, r.duration)
	defer cancel()

	if r.rampUp {
		// Run with ramp-up: gradually increase workers over ramp-up duration
		r.runWithRampUp(ctx)
	} else {
		// Run with all workers immediately
		for i := 0; i < r.numUsers; i++ {
			r.wg.Add(1)
			go r.worker(ctx)
		}
	}

	// Wait for all workers to complete
	r.wg.Wait()

	// Calculate success rate
	if r.metrics.TotalRequests > 0 {
		r.metrics.SuccessRate = float64(r.metrics.SuccessCount) / float64(r.metrics.TotalRequests) * 100
	}

	return r.metrics
}

// runWithRampUp gradually increases the number of workers from 0 to numUsers over rampUpDuration.
func (r *Runner) runWithRampUp(ctx context.Context) {
	startTime := time.Now()
	workersStarted := 0

	// Start ramp-up ticker
	rampUpTicker := time.NewTicker(100 * time.Millisecond)
	defer rampUpTicker.Stop()

	rampUpStarted := false

	for {
		select {
		case <-rampUpTicker.C:
			elapsed := time.Since(startTime)

			if !rampUpStarted {
				rampUpStarted = true
				// Start first worker immediately
				r.wg.Add(1)
				go r.worker(ctx)
				workersStarted = 1
				continue
			}

			if elapsed < r.rampUpDuration {
				// Ramp-up phase: gradually increase workers
				targetWorkers := int(float64(r.numUsers) * elapsed.Seconds() / r.rampUpDuration.Seconds())
				if targetWorkers < 1 {
					targetWorkers = 1
				}

				for workersStarted < targetWorkers && workersStarted < r.numUsers {
					r.wg.Add(1)
					go r.worker(ctx)
					workersStarted++
				}
			} else {
				// Ramp-up complete: start remaining workers
				for workersStarted < r.numUsers {
					r.wg.Add(1)
					go r.worker(ctx)
					workersStarted++
				}
				rampUpTicker.Stop()
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// worker is a worker goroutine that continuously makes requests while semaphore slots are available.
func (r *Runner) worker(ctx context.Context) {
	defer r.wg.Done()

	for {
		// Check if context is done
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Try to acquire a semaphore slot
		select {
		case r.semaphore <- struct{}{}:
			// Slot acquired, make request in background
			go r.makeRequest()
		case <-ctx.Done():
			return
		}
	}
}

// makeRequest makes a single HTTP request and releases the semaphore slot.
func (r *Runner) makeRequest() {
	defer func() { <-r.semaphore }() // Always release the slot

	// Generate request
	req, err := r.requestGen()
	if err != nil {
		r.recordResult(Result{
			Success: false,
			Error:   fmt.Sprintf("request generation failed: %v", err),
		})
		return
	}

	// Create HTTP request
	httpReq, err := http.NewRequest(req.Method, req.URL, nil)
	if err != nil {
		r.recordResult(Result{
			Success: false,
			Error:   fmt.Sprintf("failed to create http request: %v", err),
		})
		return
	}

	// Set headers
	if req.Headers != nil {
		httpReq.Header = req.Headers
	}

	// Set body if present
	if len(req.Body) > 0 {
		httpReq.Body = io.NopCloser(bytes.NewReader(req.Body))
		httpReq.ContentLength = int64(len(req.Body))
	}

	// Make request and measure latency
	start := time.Now()
	resp, err := r.client.Do(httpReq)
	latency := time.Since(start)

	// Handle request error
	if err != nil {
		r.recordResult(Result{
			Success: false,
			Error:   fmt.Sprintf("request failed: %v", err),
			Latency: latency,
		})
		return
	}
	defer resp.Body.Close()

	// Record result
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	r.recordResult(Result{
		StatusCode: resp.StatusCode,
		Latency:    latency,
		Success:    success,
	})
}

// recordResult safely records a result and updates metrics.
func (r *Runner) recordResult(result Result) {
	r.metrics.mu.Lock()
	defer r.metrics.mu.Unlock()

	r.metrics.TotalRequests++
	if result.Success {
		r.metrics.SuccessCount++
	} else {
		r.metrics.FailureCount++
	}

	// Track latency metrics
	r.metrics.TotalLatency += result.Latency
	if result.Latency > r.metrics.MaxLatency {
		r.metrics.MaxLatency = result.Latency
	}
	if r.metrics.MinLatency == 0 || result.Latency < r.metrics.MinLatency {
		r.metrics.MinLatency = result.Latency
	}

	r.metrics.Results = append(r.metrics.Results, result)
}
