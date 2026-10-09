package runtimecontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SBM_TEST_SINGBOX 必须指向官方兼容内核。测试仅监听 loopback，并只管理自身创建的进程。
// 例：SBM_TEST_SINGBOX=/path/to/sing-box go test -race ./internal/runtimecontrol -run TestSingBoxIntegration -v
func TestSingBoxIntegration(t *testing.T) {
	binary := os.Getenv("SBM_TEST_SINGBOX")
	if binary == "" {
		t.Skip("设置 SBM_TEST_SINGBOX 后执行隔离的真实内核验收")
	}
	if !filepath.IsAbs(binary) {
		t.Fatal("SBM_TEST_SINGBOX 必须使用绝对路径")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatal(err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "connected\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(target.Close)
	targetAddress := strings.TrimPrefix(target.URL, "http://")
	proxyA, countA := integrationProxy(t, targetAddress)
	proxyB, countB := integrationProxy(t, targetAddress)

	controllerAddress := integrationLoopbackAddress(t)
	mixedAddress := integrationLoopbackAddress(t)
	_, mixedPort := integrationHostPort(t, mixedAddress)
	proxyAHost, proxyAPort := integrationHostPort(t, proxyA.Listener.Addr().String())
	proxyBHost, proxyBPort := integrationHostPort(t, proxyB.Listener.Addr().String())
	const group = "代理 /?%25#组"
	const nodeA = "香港/50% 节点 A"
	const nodeB = "日本/备用% 节点 B"
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.json")
	cachePath := filepath.Join(directory, "cache.db")
	config := map[string]any{
		"log": map[string]any{"level": "info"},
		"experimental": map[string]any{
			"clash_api":  map[string]any{"external_controller": controllerAddress, "secret": "integration-only-secret"},
			"cache_file": map[string]any{"enabled": true, "path": cachePath},
		},
		"inbounds": []any{map[string]any{"type": "mixed", "tag": "integration-mixed", "listen": "127.0.0.1", "listen_port": mixedPort}},
		"outbounds": []any{
			map[string]any{"type": "selector", "tag": group, "outbounds": []string{nodeA, nodeB}, "default": nodeA, "interrupt_exist_connections": false},
			map[string]any{"type": "http", "tag": nodeA, "server": proxyAHost, "server_port": proxyAPort},
			map[string]any{"type": "http", "tag": nodeB, "server": proxyBHost, "server_port": proxyBPort},
		},
		"route": map[string]any{"final": group},
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(controllerAddress, "integration-only-secret")
	if err != nil {
		t.Fatal(err)
	}
	process := startIntegrationKernel(t, binary, configPath, directory, client)
	initialPID := process.command.Process.Pid
	version, err := client.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("真实内核：%s；隔离进程 PID=%d，controller=%s，mixed=%s", version, initialPID, controllerAddress, mixedAddress)
	assertIntegrationSelection(t, client, group, nodeA)

	first := startIntegrationStream(t, mixedAddress, target.URL+"/first")
	var firstID string
	integrationEventually(t, process, "连接 A 出现在内核快照", func() bool {
		snapshot, err := client.Connections(context.Background())
		if err != nil {
			return false
		}
		for _, connection := range snapshot.Connections {
			if slices.Contains(connection.Chains, nodeA) && slices.Contains(connection.Chains, group) {
				firstID = connection.ID
				return countA.Load() == 1 && countB.Load() == 0
			}
		}
		return false
	})
	if err := client.Select(context.Background(), group, nodeB); err != nil {
		t.Fatalf("热切换失败：%v\n%s", err, process.output.String())
	}
	if process.command.Process.Pid != initialPID {
		t.Fatal("热切换不应更换内核进程")
	}
	select {
	case <-process.done:
		t.Fatalf("热切换期间内核退出：%s", process.output.String())
	default:
	}
	assertIntegrationSelection(t, client, group, nodeB)
	assertIntegrationStreamAlive(t, first, "切换后旧连接 A")
	second := startIntegrationStream(t, mixedAddress, target.URL+"/second")
	var secondID string
	integrationEventually(t, process, "新连接实际经过出口 B，旧连接 A 仍存活", func() bool {
		snapshot, err := client.Connections(context.Background())
		if err != nil {
			return false
		}
		firstPresent := false
		for _, connection := range snapshot.Connections {
			if connection.ID == firstID {
				firstPresent = true
			}
			if slices.Contains(connection.Chains, nodeB) {
				secondID = connection.ID
			}
		}
		return firstPresent && secondID != "" && countA.Load() == 1 && countB.Load() == 1
	})
	t.Log("热切换已验证：PID 不变，两个本地出口分别观察到 1 个连接，旧连接仍通过 A，新连接通过 B")

	if err := client.Close(context.Background(), firstID); err != nil {
		t.Fatalf("关闭指定连接失败：%v", err)
	}
	select {
	case <-first.done:
	case <-time.After(3 * time.Second):
		t.Fatal("关闭连接 A 后，实际 HTTP 流仍未断开")
	}
	integrationEventually(t, process, "连接 A 已移除且连接 B 保持", func() bool {
		snapshot, err := client.Connections(context.Background())
		if err != nil {
			return false
		}
		secondPresent := false
		for _, connection := range snapshot.Connections {
			if connection.ID == firstID {
				return false
			}
			if connection.ID == secondID {
				secondPresent = true
			}
		}
		return secondPresent
	})
	assertIntegrationStreamAlive(t, second, "关闭 A 后的连接 B")
	t.Log("定向断连已验证：A 的真实 HTTP 流结束，B 的 HTTP 流和内核连接保持")

	process.stop(t)
	if info, err := os.Stat(cachePath); err != nil || info.Size() == 0 {
		t.Fatalf("选择缓存未写入：%v", err)
	}
	restarted := startIntegrationKernel(t, binary, configPath, directory, client)
	if restarted.command.Process.Pid == initialPID {
		t.Fatal("重启验收需要新进程")
	}
	assertIntegrationSelection(t, client, group, nodeB)
	third := startIntegrationStream(t, mixedAddress, target.URL+"/after-restart")
	integrationEventually(t, restarted, "重启后新连接仍经缓存选择的出口 B", func() bool {
		return countA.Load() == 1 && countB.Load() == 2
	})
	assertIntegrationStreamAlive(t, third, "重启后连接")
	t.Logf("持久化已验证：新 PID=%d 自动恢复 B，配置默认值仍是 A，重启后的新连接实际通过 B", restarted.command.Process.Pid)
}

type integrationOutput struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (output *integrationOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.buffer.Write(data)
}

func (output *integrationOutput) String() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.buffer.String()
}

type integrationKernel struct {
	command *exec.Cmd
	output  *integrationOutput
	done    chan struct{}
	once    sync.Once
}

func startIntegrationKernel(t *testing.T, binary, config, directory string, client *Client) *integrationKernel {
	t.Helper()
	process := &integrationKernel{
		command: exec.Command(binary, "run", "-c", config),
		output:  &integrationOutput{},
		done:    make(chan struct{}),
	}
	process.command.Dir = directory
	process.command.Stdout = process.output
	process.command.Stderr = process.output
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = process.command.Wait()
		close(process.done)
	}()
	t.Cleanup(func() { process.stop(t) })
	integrationEventually(t, process, "独立内核控制接口就绪", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		_, err := client.Version(ctx)
		return err == nil
	})
	return process
}

func (process *integrationKernel) stop(t *testing.T) {
	t.Helper()
	process.once.Do(func() {
		select {
		case <-process.done:
			return
		default:
		}
		_ = process.command.Process.Signal(os.Interrupt)
		select {
		case <-process.done:
			return
		case <-time.After(4 * time.Second):
			_ = process.command.Process.Kill()
		}
		select {
		case <-process.done:
		case <-time.After(2 * time.Second):
			t.Error("自行启动的内核进程未及时退出")
		}
	})
}

func integrationEventually(t *testing.T, process *integrationKernel, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-process.done:
			t.Fatalf("%s：内核意外退出\n%s", description, process.output.String())
		default:
		}
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s\n%s", description, process.output.String())
}

func integrationLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func integrationHostPort(t *testing.T, address string) (string, int) {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return host, value
}

func integrationProxy(t *testing.T, targetAddress string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	count := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != targetAddress {
			http.Error(w, "仅允许测试目标", http.StatusForbidden)
			return
		}
		upstream, err := net.DialTimeout("tcp", targetAddress, time.Second)
		if err != nil {
			http.Error(w, "测试目标不可用", http.StatusBadGateway)
			return
		}
		defer upstream.Close()
		connection, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer connection.Close()
		if _, err := buffered.WriteString("HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
			return
		}
		if err := buffered.Flush(); err != nil {
			return
		}
		count.Add(1)
		copyDone := make(chan struct{})
		go func() {
			_, _ = io.Copy(upstream, buffered)
			_ = upstream.Close()
			_ = connection.Close()
			close(copyDone)
		}()
		_, _ = io.Copy(connection, upstream)
		_ = connection.Close()
		<-copyDone
	}))
	t.Cleanup(server.Close)
	return server, count
}

type integrationStream struct {
	done chan error
}

func startIntegrationStream(t *testing.T, mixedAddress, target string) *integrationStream {
	t.Helper()
	proxyURL := &url.URL{Scheme: "http", Host: mixedAddress}
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 3 * time.Second,
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport}
	response, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != http.StatusOK {
		t.Fatalf("流请求失败：HTTP %d", response.StatusCode)
	}
	marker := make([]byte, len("connected\n"))
	if _, err := io.ReadFull(response.Body, marker); err != nil || string(marker) != "connected\n" {
		t.Fatalf("真实目标未响应：%q, %v", marker, err)
	}
	stream := &integrationStream{done: make(chan error, 1)}
	go func() {
		_, err := io.Copy(io.Discard, response.Body)
		stream.done <- err
	}()
	return stream
}

func assertIntegrationStreamAlive(t *testing.T, stream *integrationStream, description string) {
	t.Helper()
	select {
	case err := <-stream.done:
		t.Fatalf("%s 已意外断开：%v", description, err)
	default:
	}
}

func assertIntegrationSelection(t *testing.T, client *Client, group, expected string) {
	t.Helper()
	proxies, err := client.Proxies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, proxy := range proxies {
		if proxy.Tag == group {
			if !proxy.Selectable || proxy.Selected != expected {
				t.Fatalf("选择不符：%#v，预期 %q", proxy, expected)
			}
			return
		}
	}
	t.Fatalf("实际内核中缺少代理组 %q", group)
}

func TestSingBoxIntegrationExplicitGLOBAL(t *testing.T) {
	binary := os.Getenv("SBM_TEST_SINGBOX")
	if binary == "" {
		t.Skip("set SBM_TEST_SINGBOX for real GLOBAL selector verification")
	}
	controller := integrationLoopbackAddress(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	members := []string{"DIRECT", "REJECT", "Proxy", "🇺🇸 美国", "node-a", "node-b"}
	config := map[string]any{
		"experimental": map[string]any{"clash_api": map[string]any{"external_controller": controller, "secret": "global-fixture"}},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "DIRECT"}, map[string]any{"type": "block", "tag": "REJECT"},
			map[string]any{"type": "http", "tag": "node-a", "server": "127.0.0.1", "server_port": 9}, map[string]any{"type": "http", "tag": "node-b", "server": "127.0.0.1", "server_port": 19},
			map[string]any{"type": "selector", "tag": "Proxy", "outbounds": []string{"node-a", "node-b"}, "default": "node-a"},
			map[string]any{"type": "selector", "tag": "🇺🇸 美国", "outbounds": []string{"node-a"}},
			map[string]any{"type": "selector", "tag": "GLOBAL", "outbounds": members, "default": "Proxy"},
		},
		"route": map[string]any{"final": "Proxy"},
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(controller, "global-fixture")
	if err != nil {
		t.Fatal(err)
	}
	process := startIntegrationKernel(t, binary, path, directory, client)
	proxies, err := client.Proxies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	globalCount := 0
	for _, proxy := range proxies {
		if proxy.Tag == "GLOBAL" {
			globalCount++
			if !proxy.Selectable || !slices.Equal(proxy.Members, members) {
				t.Fatalf("GLOBAL still synthetic: %+v", proxy)
			}
		}
	}
	if globalCount != 1 {
		t.Fatal("GLOBAL must occur exactly once")
	}
	for _, member := range []string{"DIRECT", "REJECT", "🇺🇸 美国", "node-b", "Proxy"} {
		if err := client.Select(context.Background(), "GLOBAL", member); err != nil {
			t.Fatalf("select %s: %v\n%s", member, err, process.output.String())
		}
		assertIntegrationSelection(t, client, "GLOBAL", member)
	}
	assertIntegrationSelection(t, client, "Proxy", "node-a")
}
