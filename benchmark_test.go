package main

import (
	"sync"
	"testing"

	vegeta "github.com/tsenart/vegeta/v12/lib"
)

// TestTargetersAssignUniqueRequestIndexes calls each targeter from many
// goroutines at once, as vegeta workers and concurrent users do, and checks
// that no two requests get the same #{request_index}. Run with -race.
func TestTargetersAssignUniqueRequestIndexes(t *testing.T) {
	provider := Provider{Name: "Bifrost", PayloadTemplate: "#{request_index}"}
	targeter := createTargeter(provider)
	concurrentTargeter := createConcurrentTargeter(provider)

	generators := map[string]func() (string, error){
		"vegeta": func() (string, error) {
			var tgt vegeta.Target
			err := targeter(&tgt)
			return string(tgt.Body), err
		},
		"concurrent": func() (string, error) {
			req, err := concurrentTargeter()
			return string(req.Body), err
		},
	}

	const goroutines, perGoroutine = 50, 200
	for name, generate := range generators {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			seen := make(map[string]bool)
			var wg sync.WaitGroup
			for i := 0; i < goroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < perGoroutine; j++ {
						body, err := generate()
						if err != nil {
							t.Error(err)
							return
						}
						mu.Lock()
						if seen[body] {
							t.Errorf("duplicate request index %s", body)
						}
						seen[body] = true
						mu.Unlock()
					}
				}()
			}
			wg.Wait()
			if len(seen) != goroutines*perGoroutine {
				t.Fatalf("got %d unique indexes, want %d", len(seen), goroutines*perGoroutine)
			}
		})
	}
}
