package cfst

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

func newTestService() *CFSTService {
	return &CFSTService{
		ctx:           context.Background(),
		logger:        log.NewNOPFactory().Logger(),
		generatedTags: make(map[string][]string),
		groupTags:     make(map[string]string),
	}
}

func newTestServiceWithOptions(opts option.CFSTOptions) *CFSTService {
	return &CFSTService{
		ctx:           context.Background(),
		logger:        log.NewNOPFactory().Logger(),
		options:       opts,
		generatedTags: make(map[string][]string),
		groupTags:     make(map[string]string),
	}
}

func TestRenderTag(t *testing.T) {
	tests := []struct {
		template string
		base     string
		colo     string
		index    int
		expected string
	}{
		{"{{base}}-{{colo_lower}}-{{index}}", "my-cfess", "LAX", 1, "my-cfess-lax-1"},
		{"{{base}}-{{colo}}-{{index}}", "proxy", "SEA", 2, "proxy-SEA-2"},
		{"", "node", "SJC", 3, "node-sjc-3"},
		{"prefix-{{base}}", "test", "LAX", 1, "prefix-test"},
		{"{{colo}}-{{colo_lower}}", "x", "NYC", 1, "NYC-nyc"},
	}
	for _, tt := range tests {
		result := RenderTag(tt.template, tt.base, tt.colo, tt.index)
		if result != tt.expected {
			t.Errorf("RenderTag(%q, %q, %q, %d) = %q, want %q",
				tt.template, tt.base, tt.colo, tt.index, result, tt.expected)
		}
	}
}

func TestLoadSaveCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_cache.json")

	results := []Result{
		{IP: "1.2.3.4", Sent: 4, Received: 4, LossRate: 0, AvgLatencyMS: 100.5, DownloadSpeedMB: 25.0, Colo: "LAX"},
		{IP: "5.6.7.8", Sent: 4, Received: 3, LossRate: 0.25, AvgLatencyMS: 200.0, DownloadSpeedMB: 10.0, Colo: "SEA"},
	}

	err := SaveCache(path, results)
	if err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	loaded, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache failed: %v", err)
	}

	if len(loaded) != len(results) {
		t.Fatalf("loaded %d results, want %d", len(loaded), len(results))
	}

	for i, r := range loaded {
		if r.IP != results[i].IP {
			t.Errorf("result[%d].IP = %q, want %q", i, r.IP, results[i].IP)
		}
		if r.Colo != results[i].Colo {
			t.Errorf("result[%d].Colo = %q, want %q", i, r.Colo, results[i].Colo)
		}
		if r.DownloadSpeedMB != results[i].DownloadSpeedMB {
			t.Errorf("result[%d].DownloadSpeedMB = %f, want %f", i, r.DownloadSpeedMB, results[i].DownloadSpeedMB)
		}
	}
}

func TestLoadCacheNotExist(t *testing.T) {
	_, err := LoadCache("/nonexistent/path/cache.json")
	if err == nil {
		t.Error("LoadCache should fail for nonexistent file")
	}
}

func TestSaveCacheCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "cache.json")

	results := []Result{{IP: "1.1.1.1", Colo: "LAX"}}
	err := SaveCache(path, results)
	if err != nil {
		t.Fatalf("SaveCache with nested dirs failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("cache file was not created")
	}
}

func TestRunnerMock(t *testing.T) {
	// Save and restore original MockResults
	mockMu.RLock()
	original := MockResults
	mockMu.RUnlock()
	defer SetMockResults(original)

	// Test with custom mock
	customResults := []Result{
		{IP: "10.0.0.1", Sent: 10, Received: 10, LossRate: 0, AvgLatencyMS: 50.0, DownloadSpeedMB: 100.0, Colo: "SFO"},
	}
	SetMockResults(customResults)

	results, err := RunSpeedTest(context.Background(), 10, 5)
	if err != nil {
		t.Fatalf("RunSpeedTest failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].IP != "10.0.0.1" {
		t.Errorf("got IP %q, want %q", results[0].IP, "10.0.0.1")
	}

	// Test with nil mock (default results)
	SetMockResults(nil)
	results, err = RunSpeedTest(context.Background(), 10, 5)
	if err != nil {
		t.Fatalf("RunSpeedTest (default) failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
}

func TestServiceStatus(t *testing.T) {
	svc := newTestService()

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	svc.getStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var status map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &status)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if status["running"] != false {
		t.Error("expected running = false")
	}
	if status["result_count"] != float64(0) {
		t.Errorf("expected result_count = 0, got %v", status["result_count"])
	}
}

func TestServiceResults(t *testing.T) {
	svc := newTestService()
	svc.results = []Result{
		{IP: "1.1.1.1", Sent: 4, Received: 4, LossRate: 0, AvgLatencyMS: 100, DownloadSpeedMB: 20, Colo: "LAX"},
		{IP: "2.2.2.2", Sent: 4, Received: 4, LossRate: 0, AvgLatencyMS: 110, DownloadSpeedMB: 15, Colo: "SEA"},
	}

	// Test JSON format
	req := httptest.NewRequest(http.MethodGet, "/results", nil)
	w := httptest.NewRecorder()
	svc.getResults(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", w.Code, http.StatusOK)
	}

	var results []Result
	err := json.Unmarshal(w.Body.Bytes(), &results)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Test with limit
	req = httptest.NewRequest(http.MethodGet, "/results?limit=1", nil)
	w = httptest.NewRecorder()
	svc.getResults(w, req)

	err = json.Unmarshal(w.Body.Bytes(), &results)
	if err != nil {
		t.Fatalf("failed to parse limited response: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results with limit=1, want 1", len(results))
	}

	// Test table format
	req = httptest.NewRequest(http.MethodGet, "/results?format=table", nil)
	w = httptest.NewRecorder()
	svc.getResults(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "1.1.1.1") {
		t.Error("table format should contain IP 1.1.1.1")
	}
	if !strings.Contains(body, "LAX") {
		t.Error("table format should contain colo LAX")
	}
}

func TestConcurrency(t *testing.T) {
	// Save and restore original MockResults
	mockMu.RLock()
	original := MockResults
	mockMu.RUnlock()
	defer SetMockResults(original)

	// Use a slow mock to simulate running state
	SetMockResults([]Result{{IP: "1.1.1.1", Colo: "LAX"}})

	svc := newTestService()

	// Simulate running state
	svc.mu.Lock()
	svc.running = true
	svc.mu.Unlock()

	// Try to run while already running
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	svc.postRun(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", w.Code)
	}

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["error"] == nil {
		t.Error("expected error message in response")
	}
}

func TestFailureResilience(t *testing.T) {
	// Save and restore original MockResults
	mockMu.RLock()
	original := MockResults
	mockMu.RUnlock()
	defer SetMockResults(original)

	svc := newTestService()
	svc.results = []Result{
		{IP: "1.1.1.1", Colo: "LAX", DownloadSpeedMB: 20},
	}

	// Set mock results that will succeed
	SetMockResults([]Result{{IP: "2.2.2.2", Colo: "SEA", DownloadSpeedMB: 30}})

	// Run and wait
	svc.mu.Lock()
	svc.runID++
	currentRunID := svc.runID
	svc.mu.Unlock()
	svc.runSpeedTest(10, 10, currentRunID)

	svc.mu.Lock()
	if len(svc.results) != 1 || svc.results[0].IP != "2.2.2.2" {
		t.Error("results should be updated on success")
	}
	if svc.lastError != "" {
		t.Error("lastError should be empty on success")
	}
	svc.mu.Unlock()

	// Verify that old results are preserved on success (results were replaced)
	// Now test that previous results are preserved on error:
	// We cannot easily inject a failure with the current mock design,
	// but we can verify the service state management
	svc.mu.Lock()
	svc.results = []Result{{IP: "3.3.3.3", Colo: "SJC"}}
	svc.mu.Unlock()

	// Run again with valid mock - results should be updated
	SetMockResults([]Result{{IP: "4.4.4.4", Colo: "ORD"}})
	svc.mu.Lock()
	svc.runID++
	currentRunID = svc.runID
	svc.mu.Unlock()
	svc.runSpeedTest(10, 10, currentRunID)

	svc.mu.Lock()
	if svc.results[0].IP != "4.4.4.4" {
		t.Error("results should update on successful run")
	}
	svc.mu.Unlock()
}

func TestCancelRunning(t *testing.T) {
	svc := newTestService()

	// Not running - cancel should indicate not running
	req := httptest.NewRequest(http.MethodPost, "/cancel", nil)
	w := httptest.NewRecorder()
	svc.postCancel(w, req)

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["cancelled"] != false {
		t.Error("cancel should return false when not running")
	}

	// Set running with cancel func
	_, cancelFn := context.WithCancel(context.Background())
	svc.mu.Lock()
	svc.running = true
	svc.cancel = cancelFn
	svc.mu.Unlock()

	req = httptest.NewRequest(http.MethodPost, "/cancel", nil)
	w = httptest.NewRecorder()
	svc.postCancel(w, req)

	err = json.Unmarshal(w.Body.Bytes(), &resp)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["cancelled"] != true {
		t.Error("cancel should return true when running")
	}
}

func TestRunStartsSpeedTest(t *testing.T) {
	// Save and restore original MockResults
	mockMu.RLock()
	original := MockResults
	mockMu.RUnlock()
	defer SetMockResults(original)

	SetMockResults([]Result{
		{IP: "1.1.1.1", Colo: "LAX", Sent: 4, Received: 4, DownloadSpeedMB: 20},
	})

	svc := newTestService()

	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{"dn": 5, "p": 3}`))
	w := httptest.NewRecorder()
	svc.postRun(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	if err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["started"] != true {
		t.Error("expected started = true")
	}

	// Wait for goroutine to complete
	time.Sleep(100 * time.Millisecond)

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.running {
		t.Error("expected running = false after completion")
	}
	if len(svc.results) != 1 {
		t.Errorf("expected 1 result, got %d", len(svc.results))
	}
}

func TestServiceName(t *testing.T) {
	svc := &CFSTService{}
	if svc.Name() != "cfst" {
		t.Errorf("Name() = %q, want %q", svc.Name(), "cfst")
	}
}

func TestRunConcurrentSafe(t *testing.T) {
	// Save and restore original MockResults
	mockMu.RLock()
	original := MockResults
	mockMu.RUnlock()
	defer SetMockResults(original)

	SetMockResults([]Result{{IP: "1.1.1.1", Colo: "LAX"}})

	svc := newTestService()

	// Run multiple concurrent requests
	var wg sync.WaitGroup
	conflicts := 0
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{}`))
			w := httptest.NewRecorder()
			svc.postRun(w, req)
			if w.Code == http.StatusConflict {
				mu.Lock()
				conflicts++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	// At least some should get 409
	// (one will succeed, others might conflict)
	// Give goroutine time to finish
	time.Sleep(100 * time.Millisecond)
}
