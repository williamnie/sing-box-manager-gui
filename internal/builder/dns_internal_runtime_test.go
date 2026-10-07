package builder

import (
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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// 两个回环 DNS 给出不同地址，真实内核的 resolve 动作必须选择代理解析器。
// 不启动 TUN/TProxy、不访问外网，也不依赖字段断言代替真实 HTTP 连接。
func TestRealKernelDNSBypassInternalResolution(t *testing.T) {
	binaryPath := os.Getenv("SBM_TEST_SINGBOX")
	if binaryPath == "" {
		t.Skip("set SBM_TEST_SINGBOX for loopback DNS integration")
	}
	var directQueries, proxyQueries atomic.Int64
	directDNS := startAnswerDNS(t, [4]byte{127, 0, 0, 2}, &directQueries)
	proxyDNS := startAnswerDNS(t, [4]byte{127, 0, 0, 1}, &proxyQueries)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "proxy-resolver-reached")
	}))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)

	s := dnsBypassSettings()
	s.ClashAPIPort = 0
	s.DirectDNS, s.ProxyDNS = "udp://"+directDNS, "udp://"+proxyDNS
	c, err := gwBuilder(s, nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	// 只替换网络出口为回环直连；生成器仍按真实代理配置生成 DNS 分流规则。
	c.Outbounds = []Outbound{{"type": "direct", "tag": "Proxy"}, {"type": "direct", "tag": "DIRECT"}}
	// 内核禁止把 DNS detour 指向空 direct；回环 fixture 直接访问测试 DNS。
	// 生成器本身仍须给代理 DNS 配置 Proxy 出站，在真实连接断言后核对。
	proxyDNSUsesProxy := false
	servers := c.DNS.Raw["servers"].([]any)
	for i, value := range servers {
		if server, ok := value.(DNSServer); ok && server.Tag == "dns_proxy" {
			proxyDNSUsesProxy = server.Detour == "Proxy"
			server.Detour = ""
			servers[i] = server
		}
	}
	port := freeLoopbackPort(t)
	dnsPort := freeLoopbackPort(t)
	for dnsPort == port {
		dnsPort = freeLoopbackPort(t)
	}
	c.Inbounds = []Inbound{{Type: "mixed", Tag: "mixed-in", Listen: "127.0.0.1", ListenPort: port}, {Type: "direct", Tag: "lan-dns", Listen: "127.0.0.1", ListenPort: dnsPort}}
	c.Route.DefaultInterface = ""
	c.Route.RuleSet = nil
	c.Route.Rules = []RouteRule{{"inbound": []string{"lan-dns"}, "action": "hijack-dns"}, {"action": "resolve"}, {"outbound": "Proxy"}}
	c.Route.Final = "Proxy"
	c.NTP = nil
	if c.Route.DefaultDomainResolver == nil || c.Route.DefaultDomainResolver.Server != "dns_direct" {
		t.Fatal("proxy endpoint bootstrap must remain independent of the proxy DNS")
	}
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	raw, _ := json.Marshal(c)
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	check := exec.Command(binaryPath, "check", "-c", configPath)
	check.Dir = dir
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("configuration rejected: %v\n%s", err, output)
	}
	log, err := os.Create(filepath.Join(dir, "kernel.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(binaryPath, "run", "-c", configPath)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()
	address := fmt.Sprintf("127.0.0.1:%d", dnsPort)
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		answer, err := dnsExchange("udp", address, "proxy-lookup.test", dnsmessage.TypeA)
		if err == nil && len(answer) == 1 && strings.HasPrefix(answer[0], "198.18.") {
			ready = true
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !ready {
		output, _ := os.ReadFile(filepath.Join(dir, "kernel.log"))
		t.Fatalf("LAN DNS did not provide FakeIP: %s", output)
	}
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get("http://proxy-lookup.test:" + originURL.Port() + "/")
	if err != nil {
		t.Fatalf("internal lookup did not reach proxy DNS: direct=%d proxy=%d error=%v", directQueries.Load(), proxyQueries.Load(), err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || string(body) != "proxy-resolver-reached" || proxyQueries.Load() == 0 || directQueries.Load() != 0 || !proxyDNSUsesProxy {
		t.Fatalf("wrong DNS path: status=%d body=%q direct=%d proxy=%d error=%v", response.StatusCode, body, directQueries.Load(), proxyQueries.Load(), err)
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startAnswerDNS(t *testing.T, address [4]byte, queries *atomic.Int64) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	t.Cleanup(func() { _ = conn.Close(); <-done })
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		for {
			n, peer, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
				continue
			}
			queries.Add(1)
			response := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionDesired: true, RecursionAvailable: true}, Questions: query.Questions}
			q := query.Questions[0]
			if q.Type == dnsmessage.TypeA {
				response.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: q.Name, Type: q.Type, Class: q.Class, TTL: 1}, Body: &dnsmessage.AResource{A: address}}}
			}
			packet, err := response.Pack()
			if err == nil {
				_, _ = conn.WriteTo(packet, peer)
			}
		}
	}()
	return conn.LocalAddr().String()
}
