package cfst

import (
	"context"
	"sync"
)

// Result represents a single speed test result for a Cloudflare IP.
type Result struct {
	IP              string  `json:"ip"`
	Sent            int     `json:"sent"`
	Received        int     `json:"received"`
	LossRate        float64 `json:"loss_rate"`
	AvgLatencyMS    float64 `json:"avg_latency_ms"`
	DownloadSpeedMB float64 `json:"download_speed_mb_s"`
	Colo            string  `json:"colo"`
}

var (
	mockMu      sync.RWMutex
	MockResults []Result
)

// SetMockResults sets the mock results for testing (thread-safe).
func SetMockResults(results []Result) {
	mockMu.Lock()
	MockResults = results
	mockMu.Unlock()
}

// RunSpeedTest runs a Cloudflare speed test and returns the top results.
// If MockResults is set, it returns that instead.
func RunSpeedTest(ctx context.Context, downloadCount, displayCount int) ([]Result, error) {
	mockMu.RLock()
	mock := MockResults
	mockMu.RUnlock()

	if mock != nil {
		return mock, nil
	}
	// Default mock results for development
	return []Result{
		{IP: "104.27.200.69", Sent: 4, Received: 4, LossRate: 0, AvgLatencyMS: 146.23, DownloadSpeedMB: 28.64, Colo: "LAX"},
		{IP: "172.67.60.78", Sent: 4, Received: 4, LossRate: 0, AvgLatencyMS: 139.82, DownloadSpeedMB: 15.02, Colo: "SEA"},
		{IP: "104.25.140.153", Sent: 4, Received: 4, LossRate: 0, AvgLatencyMS: 146.49, DownloadSpeedMB: 14.90, Colo: "SJC"},
	}, nil
}
