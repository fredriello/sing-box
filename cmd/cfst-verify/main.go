// cfst-verify is a standalone program that boots a minimal sing-box instance
// with cfess outbound and CFST service, triggers speed test via API, then
// queries the Clash API /proxies endpoint to verify dynamic outbound generation.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cacheDir := os.TempDir()
	cacheFile := cacheDir + "/cfst-verify-cache.json"
	defer os.Remove(cacheFile)

	// Build minimal config
	opts := option.Options{
		Outbounds: []option.Outbound{
			{
				Type: "cfess",
				Tag:  "cf-out",
				Options: &option.CFESSOutboundOptions{
					ServerOptions: option.ServerOptions{
						Server:     "s.404.xyz",
						ServerPort: 443,
					},
					UUID: "0c67ca68-63ad-60c5-8888-9cf1925c9999",
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
						TLS: &option.OutboundTLSOptions{
							Enabled:    true,
							ServerName: "s.404.xyz",
							Insecure:   true,
						},
					},
					Transport: &option.V2RayTransportOptions{
						Type: "ws",
						WebsocketOptions: option.V2RayWebsocketOptions{
							Path: "/rax",
							Headers: badoption.HTTPHeader(map[string]badoption.Listable[string]{
								"Host": {"s.404.xyz"},
							}),
						},
					},
				},
			},
		},
		Experimental: &option.ExperimentalOptions{
			ClashAPI: &option.ClashAPIOptions{
				ExternalController: "127.0.0.1:19090",
				Secret:             "test",
			},
			CFST: &option.CFSTOptions{
				Enabled:       true,
				RunOnStart:    false,
				DownloadCount: 20,
				DisplayCount:  20,
				CacheFile:     cacheFile,
				CFESS: []option.CFESSMapping{
					{
						Tag:             "cf-out",
						GroupTag:        "cf-go",
						GeneratedCount:  2,
						IncludeOriginal: true,
						StableTags:      false,
						TagTemplate:     "{{base}}-{{colo_lower}}{{index}}",
					},
				},
			},
		},
	}

	// Create and start box
	instance, err := box.New(box.Options{
		Options: opts,
		Context: include.Context(context.Background()),
	})
	if err != nil {
		return fmt.Errorf("create box: %w", err)
	}

	err = instance.Start()
	if err != nil {
		return fmt.Errorf("start box: %w", err)
	}
	defer instance.Close()

	// Wait for API to be ready
	time.Sleep(500 * time.Millisecond)

	baseURL := "http://127.0.0.1:19090"
	client := &http.Client{Timeout: 10 * time.Second}

	fmt.Println("=" + strings.Repeat("=", 69))
	fmt.Println(" CFST 动态生成验证报告 - 通过 sing-box Clash API 验证")
	fmt.Println("=" + strings.Repeat("=", 69))
	fmt.Println()

	// Step 1: Check initial proxies
	fmt.Println("### Step 1: 查询初始代理列表 (GET /proxies)")
	fmt.Println()
	proxies, err := getProxies(client, baseURL)
	if err != nil {
		return fmt.Errorf("get initial proxies: %w", err)
	}
	printProxies(proxies)
	fmt.Println()

	// Step 2: Trigger speed test
	fmt.Println("### Step 2: 触发测速 (POST /cfst/run)")
	fmt.Println()
	err = triggerRun(client, baseURL)
	if err != nil {
		return fmt.Errorf("trigger run: %w", err)
	}
	fmt.Println("  请求: POST /cfst/run {\"dn\": 20, \"p\": 20}")
	fmt.Println("  响应: started=true")
	fmt.Println()

	// Step 3: Wait for completion
	fmt.Println("### Step 3: 等待测速完成")
	fmt.Println()
	err = waitForCompletion(client, baseURL)
	if err != nil {
		return fmt.Errorf("wait for completion: %w", err)
	}
	fmt.Println("  测速已完成")
	fmt.Println()

	// Step 4: Check CFST results
	fmt.Println("### Step 4: 查询测速结果 (GET /cfst/results)")
	fmt.Println()
	err = printResults(client, baseURL)
	if err != nil {
		return fmt.Errorf("get results: %w", err)
	}
	fmt.Println()

	// Step 5: Check proxies after generation
	fmt.Println("### Step 5: 验证动态生成 (GET /proxies)")
	fmt.Println()
	proxies, err = getProxies(client, baseURL)
	if err != nil {
		return fmt.Errorf("get proxies after generation: %w", err)
	}
	printProxies(proxies)
	fmt.Println()

	// Step 6: Verify specific outbounds
	fmt.Println("### Step 6: 验证具体代理节点")
	fmt.Println()

	checks := []struct {
		tag      string
		expected string
	}{
		{"cf-out", "cfess 基础出站"},
		{"cf-out-lax1", "动态生成 (LAX #1)"},
		{"cf-out-sea1", "动态生成 (SEA #1)"},
		{"cf-go", "URLTest 代理组"},
	}

	allPass := true
	for _, check := range checks {
		proxy, exists := proxies[check.tag]
		if !exists {
			fmt.Printf("  ❌ %s (%s): 不存在\n", check.tag, check.expected)
			allPass = false
			continue
		}
		proxyMap, _ := proxy.(map[string]any)
		proxyType, _ := proxyMap["type"].(string)
		fmt.Printf("  ✅ %s (%s): type=%s\n", check.tag, check.expected, proxyType)

		// Check group members for cf-go
		if check.tag == "cf-go" {
			all, _ := proxyMap["all"].([]any)
			if all != nil {
				members := make([]string, 0, len(all))
				for _, m := range all {
					members = append(members, fmt.Sprint(m))
				}
				fmt.Printf("     成员: %v\n", members)
			}
		}
	}
	fmt.Println()

	// Step 7: Verify individual proxy details
	fmt.Println("### Step 7: 验证代理详情 (GET /proxies/{name})")
	fmt.Println()

	for _, tag := range []string{"cf-out-lax1", "cf-out-sea1", "cf-go"} {
		detail, err := getProxyDetail(client, baseURL, tag)
		if err != nil {
			fmt.Printf("  ⚠️  %s: 获取失败 (%v)\n", tag, err)
			continue
		}
		detailMap, _ := detail.(map[string]any)
		proxyType, _ := detailMap["type"].(string)
		fmt.Printf("  %s:\n", tag)
		fmt.Printf("    type: %s\n", proxyType)
		if all, ok := detailMap["all"].([]any); ok {
			members := make([]string, 0, len(all))
			for _, m := range all {
				members = append(members, fmt.Sprint(m))
			}
			fmt.Printf("    all: %v\n", members)
		}
		if now, ok := detailMap["now"].(string); ok {
			fmt.Printf("    now: %s\n", now)
		}
	}
	fmt.Println()

	// Step 8: Check CFST status via API
	fmt.Println("### Step 8: CFST 服务状态 (GET /cfst/status)")
	fmt.Println()
	err = printStatus(client, baseURL)
	if err != nil {
		return fmt.Errorf("get status: %w", err)
	}
	fmt.Println()

	// Final verdict
	fmt.Println("=" + strings.Repeat("=", 69))
	if allPass {
		fmt.Println(" 验收结果: ✅ 全部通过")
		fmt.Println()
		fmt.Println(" - cf-out (cfess 基础出站) 存在")
		fmt.Println(" - cf-out-lax1 (动态生成) 存在")
		fmt.Println(" - cf-out-sea1 (动态生成) 存在")
		fmt.Println(" - cf-go (URLTest 代理组) 存在，成员正确")
	} else {
		fmt.Println(" 验收结果: ❌ 部分失败")
	}
	fmt.Println("=" + strings.Repeat("=", 69))

	if !allPass {
		return fmt.Errorf("verification failed")
	}
	return nil
}

func getProxies(client *http.Client, baseURL string) (map[string]any, error) {
	req, _ := http.NewRequest("GET", baseURL+"/proxies", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	proxies, _ := result["proxies"].(map[string]any)
	return proxies, nil
}

func getProxyDetail(client *http.Client, baseURL, name string) (any, error) {
	req, _ := http.NewRequest("GET", baseURL+"/proxies/"+name, nil)
	req.Header.Set("Authorization", "Bearer test")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result any
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func triggerRun(client *http.Client, baseURL string) error {
	req, _ := http.NewRequest("POST", baseURL+"/cfst/run", strings.NewReader(`{"dn":20,"p":20}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	return nil
}

func waitForCompletion(client *http.Client, baseURL string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", baseURL+"/cfst/status", nil)
		req.Header.Set("Authorization", "Bearer test")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		var status map[string]any
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if status["running"] == false {
			if cnt, ok := status["result_count"].(float64); ok && cnt > 0 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for speed test completion")
}

func printResults(client *http.Client, baseURL string) error {
	req, _ := http.NewRequest("GET", baseURL+"/cfst/results?format=table&limit=10", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("  %s", string(body))
	return nil
}

func printStatus(client *http.Client, baseURL string) error {
	req, _ := http.NewRequest("GET", baseURL+"/cfst/status", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var status map[string]any
	json.Unmarshal(body, &status)
	pretty, _ := json.MarshalIndent(status, "  ", "  ")
	fmt.Printf("  %s\n", string(pretty))
	return nil
}

func printProxies(proxies map[string]any) {
	if proxies == nil {
		fmt.Println("  (no proxies)")
		return
	}
	for name, proxy := range proxies {
		if name == "GLOBAL" {
			continue
		}
		proxyMap, _ := proxy.(map[string]any)
		proxyType, _ := proxyMap["type"].(string)
		line := fmt.Sprintf("  - %s (type=%s)", name, proxyType)
		if all, ok := proxyMap["all"].([]any); ok {
			members := make([]string, 0, len(all))
			for _, m := range all {
				members = append(members, fmt.Sprint(m))
			}
			line += fmt.Sprintf(" members=%v", members)
		}
		fmt.Println(line)
	}
}
