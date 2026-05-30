package cfst

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
)

// TestIntegrationFullAPIFlow starts a real HTTP server with the CFST service
// and exercises the full API flow: status -> run -> wait -> results -> cancel.
// This simulates the real network test flow using built-in mock results.
func TestIntegrationFullAPIFlow(t *testing.T) {
	// Save and restore original MockResults
	mockMu.RLock()
	original := MockResults
	mockMu.RUnlock()
	defer SetMockResults(original)

	// Clear mock so default results are used (simulating real CFST behavior)
	SetMockResults(nil)

	// Setup cache directory
	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "cfst.json")

	// Create service with realistic options
	svc := newTestServiceWithOptions(testCFSTOptions())
	svc.options.DownloadCount = 20
	svc.options.DisplayCount = 20
	svc.options.CacheFile = cacheFile
	svc.options.CFESS = nil // No outbound generation (pure API test)

	// Start real HTTP server
	mux := http.NewServeMux()
	mux.Handle("/cfst/", http.StripPrefix("/cfst", svc.APIRouter()))
	server := &http.Server{Addr: "127.0.0.1:0"}
	server.Handler = mux

	// Use a listener to get the actual port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	addr := ln.Addr().String()
	go server.Serve(ln)
	defer server.Close()

	baseURL := "http://" + addr + "/cfst"
	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("Step1_Status_Initial", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/status")
		if err != nil {
			t.Fatalf("GET /status failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var status map[string]any
		json.NewDecoder(resp.Body).Decode(&status)

		if status["running"] != false {
			t.Errorf("expected running=false, got %v", status["running"])
		}
		if status["result_count"] != float64(0) {
			t.Errorf("expected result_count=0, got %v", status["result_count"])
		}
		t.Logf("Initial status: %+v", status)
	})

	t.Run("Step2_TriggerSpeedTest", func(t *testing.T) {
		body := strings.NewReader(`{"dn": 20, "p": 20}`)
		resp, err := client.Post(baseURL+"/run", "application/json", body)
		if err != nil {
			t.Fatalf("POST /run failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["started"] != true {
			t.Errorf("expected started=true, got %v", result["started"])
		}
		t.Logf("Run response: %+v", result)
	})

	// Wait for speed test to complete
	t.Run("Step3_WaitForCompletion", func(t *testing.T) {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := client.Get(baseURL + "/status")
			if err != nil {
				t.Fatalf("GET /status failed: %v", err)
			}
			var status map[string]any
			json.NewDecoder(resp.Body).Decode(&status)
			resp.Body.Close()

			if status["running"] == false && status["result_count"].(float64) > 0 {
				t.Logf("Speed test completed: %+v", status)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("Speed test did not complete within timeout")
	})

	t.Run("Step4_GetResults_JSON", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/results?limit=10")
		if err != nil {
			t.Fatalf("GET /results failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var results []Result
		json.NewDecoder(resp.Body).Decode(&results)

		if len(results) == 0 {
			t.Fatal("expected results, got empty")
		}
		if len(results) > 10 {
			t.Errorf("limit=10 but got %d results", len(results))
		}

		// Verify result fields
		first := results[0]
		if first.IP == "" {
			t.Error("first result IP is empty")
		}
		if first.Colo == "" {
			t.Error("first result Colo is empty")
		}
		if first.DownloadSpeedMB <= 0 {
			t.Error("first result DownloadSpeedMB should be > 0")
		}

		t.Logf("JSON results (%d items):", len(results))
		for i, r := range results {
			t.Logf("  [%d] IP=%s Sent=%d Recv=%d Loss=%.2f Latency=%.2fms Speed=%.2fMB/s Colo=%s",
				i+1, r.IP, r.Sent, r.Received, r.LossRate, r.AvgLatencyMS, r.DownloadSpeedMB, r.Colo)
		}
	})

	t.Run("Step5_GetResults_Table", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/results?format=table&limit=10")
		if err != nil {
			t.Fatalf("GET /results?format=table failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		table := string(bodyBytes)

		// Verify table headers (Chinese)
		if !strings.Contains(table, "IP") {
			t.Error("table missing IP header")
		}
		if !strings.Contains(table, "MB/s") {
			t.Error("table missing download speed header")
		}

		t.Logf("Table output:\n%s", table)
	})

	t.Run("Step6_Concurrency_409", func(t *testing.T) {
		// Set running state manually
		svc.mu.Lock()
		svc.running = true
		svc.mu.Unlock()

		body := strings.NewReader(`{}`)
		resp, err := client.Post(baseURL+"/run", "application/json", body)
		if err != nil {
			t.Fatalf("POST /run failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 409 {
			t.Errorf("expected 409, got %d", resp.StatusCode)
		}

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		t.Logf("Concurrency response: %+v (status=%d)", result, resp.StatusCode)

		// Reset
		svc.mu.Lock()
		svc.running = false
		svc.mu.Unlock()
	})

	t.Run("Step7_Cancel_NotRunning", func(t *testing.T) {
		resp, err := client.Post(baseURL+"/cancel", "application/json", nil)
		if err != nil {
			t.Fatalf("POST /cancel failed: %v", err)
		}
		defer resp.Body.Close()

		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)

		if result["cancelled"] != false {
			t.Errorf("expected cancelled=false when not running, got %v", result["cancelled"])
		}
		t.Logf("Cancel (not running) response: %+v", result)
	})

	t.Run("Step8_CacheFileWritten", func(t *testing.T) {
		// Trigger a run to write cache
		body := strings.NewReader(`{"dn": 20, "p": 20}`)
		resp, err := client.Post(baseURL+"/run", "application/json", body)
		if err != nil {
			t.Fatalf("POST /run failed: %v", err)
		}
		resp.Body.Close()

		// Wait for completion
		time.Sleep(200 * time.Millisecond)

		// Check cache file exists
		if _, err := os.Stat(cacheFile); os.IsNotExist(err) {
			t.Fatal("cache file was not written")
		}

		// Read and verify cache content
		data, err := os.ReadFile(cacheFile)
		if err != nil {
			t.Fatalf("failed to read cache file: %v", err)
		}

		var cached []Result
		err = json.Unmarshal(data, &cached)
		if err != nil {
			t.Fatalf("failed to parse cache file: %v", err)
		}

		if len(cached) == 0 {
			t.Fatal("cache file is empty")
		}

		t.Logf("Cache file written: %s (%d results)", cacheFile, len(cached))
		for i, r := range cached {
			t.Logf("  [%d] IP=%s Speed=%.2fMB/s Colo=%s", i+1, r.IP, r.DownloadSpeedMB, r.Colo)
		}
	})

	t.Run("Step9_dn_p_Parameters", func(t *testing.T) {
		// Run with specific dn and p values
		body := strings.NewReader(`{"dn": 20, "p": 2}`)
		resp, err := client.Post(baseURL+"/run", "application/json", body)
		if err != nil {
			t.Fatalf("POST /run failed: %v", err)
		}
		resp.Body.Close()

		// Wait for completion
		time.Sleep(200 * time.Millisecond)

		// Get results with limit matching p
		resp, err = client.Get(baseURL + "/results?limit=2")
		if err != nil {
			t.Fatalf("GET /results failed: %v", err)
		}
		defer resp.Body.Close()

		var results []Result
		json.NewDecoder(resp.Body).Decode(&results)

		if len(results) > 2 {
			t.Errorf("p=2 limit=2 but got %d results", len(results))
		}
		t.Logf("Results with limit=2: %d items", len(results))
	})

	t.Run("Step10_DefaultDisplayCount", func(t *testing.T) {
		// Run without p parameter (should default to display_count from options or 10)
		body := strings.NewReader(`{"dn": 20}`)
		resp, err := client.Post(baseURL+"/run", "application/json", body)
		if err != nil {
			t.Fatalf("POST /run failed: %v", err)
		}
		resp.Body.Close()

		time.Sleep(200 * time.Millisecond)

		// Verify status shows results
		resp, err = client.Get(baseURL + "/status")
		if err != nil {
			t.Fatalf("GET /status failed: %v", err)
		}
		defer resp.Body.Close()

		var status map[string]any
		json.NewDecoder(resp.Body).Decode(&status)
		t.Logf("Status after run without p: %+v", status)

		if status["result_count"].(float64) == 0 {
			t.Error("expected results after run")
		}
	})

	// Final status summary
	t.Run("Step11_FinalStatus", func(t *testing.T) {
		resp, err := client.Get(baseURL + "/status")
		if err != nil {
			t.Fatalf("GET /status failed: %v", err)
		}
		defer resp.Body.Close()

		var status map[string]any
		json.NewDecoder(resp.Body).Decode(&status)

		t.Logf("\n=== FINAL TEST REPORT ===")
		t.Logf("enabled: true (service running)")
		t.Logf("running: %v", status["running"])
		t.Logf("result_count: %v", status["result_count"])
		t.Logf("last_error: %q", status["last_error"])
		t.Logf("=========================")

		if status["last_error"] != "" {
			t.Errorf("unexpected last_error: %v", status["last_error"])
		}
	})
}

func testCFSTOptions() option.CFSTOptions {
	return option.CFSTOptions{
		Enabled:       true,
		DownloadCount: 20,
		DisplayCount:  20,
	}
}
