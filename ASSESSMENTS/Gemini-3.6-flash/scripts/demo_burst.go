package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

func main() {
	baseURL := "http://localhost:8080"
	fmt.Println("🚀 Starting Notification Service Burst Demo...")

	// 1. Health Check
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		fmt.Printf("❌ Failed to connect to server: %v\nMake sure the server is running on port 8080.\n", err)
		return
	}
	resp.Body.Close()
	fmt.Println("✅ Health check passed!")

	// 2. Submit concurrent burst of 50 requests targeting multiple recipients
	numRequests := 50
	recipientsPerReq := 5

	fmt.Printf("📦 Submitting %d requests (%d total recipients)...\n", numRequests, numRequests*recipientsPerReq)

	var requestIDs []string
	var mu sync.Mutex
	var wg sync.WaitGroup

	start := time.Now()

	channels := []string{"email", "sms", "push"}

	for i := 1; i <= numRequests; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ch := channels[id%len(channels)]
			recipients := make([]string, recipientsPerReq)
			for r := 0; r < recipientsPerReq; r++ {
				switch ch {
				case "email":
					recipients[r] = fmt.Sprintf("user_%d_%d@example.com", id, r)
				case "sms":
					recipients[r] = fmt.Sprintf("+1415555%04d", (id*10+r)%10000)
				case "push":
					recipients[r] = fmt.Sprintf("push_token_%d_%d", id, r)
				}
			}

			// Add an invalid recipient to request #5 to demonstrate partial failure tracking
			if id == 5 {
				recipients[0] = "invalid_format_address"
			}

			payload := map[string]interface{}{
				"channel":    ch,
				"recipients": recipients,
				"subject":    fmt.Sprintf("Burst Notification #%d", id),
				"body":       fmt.Sprintf("Hello from burst test job #%d!", id),
			}
			bodyBytes, _ := json.Marshal(payload)

			res, err := http.Post(baseURL+"/api/v1/notifications", "application/json", bytes.NewBuffer(bodyBytes))
			if err != nil {
				fmt.Printf("❌ Request #%d failed: %v\n", id, err)
				return
			}
			defer res.Body.Close()

			if res.StatusCode == http.StatusAccepted {
				var data map[string]interface{}
				_ = json.NewDecoder(res.Body).Decode(&data)
				if reqID, ok := data["id"].(string); ok {
					mu.Lock()
					requestIDs = append(requestIDs, reqID)
					mu.Unlock()
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)
	fmt.Printf("⚡ Submitted %d requests in %v (Avg: %.2f ms/req)\n", len(requestIDs), duration, float64(duration.Milliseconds())/float64(numRequests))

	// 3. Wait for background workers to complete processing
	fmt.Println("⏳ Waiting 1.5 seconds for async worker pool to finish processing...")
	time.Sleep(1500 * time.Millisecond)

	// 4. Inspect status of sample requests
	fmt.Println("🔍 Inspecting statuses of submitted requests:")
	for idx, reqID := range requestIDs[:5] {
		res, err := http.Get(fmt.Sprintf("%s/api/v1/notifications/%s", baseURL, reqID))
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		fmt.Printf("  Sample #%d [ID: %s]: %s\n", idx+1, reqID[:8], string(body))
	}

	// 5. List all notifications
	listRes, err := http.Get(baseURL + "/api/v1/notifications?limit=5")
	if err == nil {
		body, _ := io.ReadAll(listRes.Body)
		listRes.Body.Close()
		fmt.Printf("📋 Paginated List Sample: %s\n", string(body))
	}

	fmt.Println("🎉 Burst Demo complete!")
}
