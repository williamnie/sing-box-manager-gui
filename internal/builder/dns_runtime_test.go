package builder

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/storage"
	"golang.org/x/net/dns/dnsmessage"
)

// 真正启动目标内核的 DNS 引擎，仅在回环高端口运行；不启动 TUN/TProxy 或操作宿主路由。
// IPv4/IPv6 回环来源模拟两个终端，证明来源判定不会被同名 DNS 缓存覆盖。
func TestRealKernelDNSBypassAnswersAndRestart(t *testing.T) {
	binaryPath := os.Getenv("SBM_TEST_SINGBOX")
	if binaryPath == "" {
		t.Skip("set SBM_TEST_SINGBOX for loopback DNS integration")
	}
	s := dnsBypassSettings()
	s.ClashAPIPort = 0
	s.Hosts = []storage.HostEntry{{Domain: "nas.test", IPs: []string{"192.0.2.9"}, Enabled: true}}
	s.DeviceGroups = []storage.DeviceGroup{{ID: "d", Policy: "direct"}}
	s.Devices = []storage.Device{{ID: "d", GroupID: "d", Enabled: true, Addresses: []string{"192.0.2.10"}}}
	s.SplitDNS = []storage.SplitDNSRule{{DomainSuffix: []string{"lan.test"}, Server: "direct"}}
	s.ImportedPolicy = &storage.ImportedPolicy{
		Outbounds: []map[string]any{{"type": "direct", "tag": "DIRECT"}, {"type": "socks", "tag": "Proxy", "server": "203.0.113.8", "server_port": 1080}},
		Rules: []map[string]any{
			{"type": "logical", "mode": "or", "rules": []any{map[string]any{"domain": []string{"logic.test"}}, map[string]any{"domain": []string{"logic2.test"}}}, "outbound": "Proxy"},
			{"domain": []string{"override.test"}, "override_address": "192.0.2.8", "outbound": "DIRECT"},
			{"domain": []string{"keep-direct.test"}, "invert": true, "outbound": "Proxy"},
		}, Final: "DIRECT",
		DNS: map[string]any{"servers": []any{map[string]any{"type": "udp", "tag": "remote-resolver", "server": "127.0.0.1", "server_port": 9}}, "rules": []any{map[string]any{"domain": []string{"foreign.test"}, "server": "remote-resolver"}}},
	}
	c, err := gwBuilder(s, []storage.Rule{{Enabled: true, RuleType: "domain_suffix", Values: []string{"cn.test"}, Outbound: "DIRECT"}}).Build()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	c.Inbounds = []Inbound{{Type: "direct", Tag: "lan-dns", Listen: "::", ListenPort: port}}
	c.NTP = nil
	c.Route.DefaultInterface = ""
	// fixture 不运行生成组的后台连通性探测，避免 DNS 测试触发外网连接。
	for _, outbound := range c.Outbounds {
		if outbound["type"] == "urltest" {
			outbound["type"] = "selector"
			for _, key := range []string{"url", "interval", "tolerance", "idle_timeout"} {
				delete(outbound, key)
			}
		}
	}
	// 回环测试不依赖外网，将真实地址解析器替换为确定性的 typed hosts 上游。
	servers := c.DNS.Raw["servers"].([]any)
	for i, v := range servers {
		if server, ok := v.(DNSServer); ok && server.Tag == "dns_direct" {
			servers[i] = DNSServer{Type: "hosts", Tag: "dns_direct", Predefined: map[string]any{"foreign.test": "203.0.113.9", "cn.test": "203.0.113.10", "lan.test": "192.0.2.11"}}
		}
	}
	raw, _ := json.Marshal(c)
	raw = []byte(strings.ReplaceAll(string(raw), "192.0.2.10", "::1/128"))
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	check := exec.Command(binaryPath, "check", "-c", path)
	check.Dir = dir
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("DNS config rejected: %v\n%s", err, output)
	}
	start := func() func() {
		log, err := os.CreateTemp(dir, "kernel-log-")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binaryPath, "run", "-c", path)
		cmd.Dir = dir
		cmd.Stdout = log
		cmd.Stderr = log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				_ = cmd.Process.Signal(os.Interrupt)
				_ = cmd.Wait()
				_ = log.Close()
			}
		}
		t.Cleanup(stop)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := dnsExchange("udp", fmt.Sprintf("127.0.0.1:%d", port), "nas.test", dnsmessage.TypeA); err == nil {
				return stop
			}
			time.Sleep(50 * time.Millisecond)
		}
		stop()
		output, _ := os.ReadFile(log.Name())
		t.Fatalf("DNS did not become ready: %s", output)
		return stop
	}
	query := func(network, host, domain string, qtype dnsmessage.Type) []string {
		t.Helper()
		ips, err := dnsExchange(network, net.JoinHostPort(host, fmt.Sprint(port)), domain, qtype)
		if err != nil {
			t.Fatal(err)
		}
		return ips
	}
	stop := start()
	for _, network := range []string{"udp", "tcp"} {
		for domain, want := range map[string]string{"nas.test": "192.0.2.9", "cn.test": "203.0.113.10", "lan.test": "192.0.2.11"} {
			if got := query(network, "127.0.0.1", domain, dnsmessage.TypeA); len(got) != 1 || got[0] != want {
				t.Fatalf("%s %s got %v want %s", network, domain, got, want)
			}
		}
	}
	fake := query("udp", "127.0.0.1", "foreign.test", dnsmessage.TypeA)
	if len(fake) != 1 || !strings.HasPrefix(fake[0], "198.18.") {
		t.Fatalf("expected FakeIP: %v", fake)
	}
	for _, domain := range []string{"logic.test", "logic2.test", "override.test"} {
		if got := query("udp", "127.0.0.1", domain, dnsmessage.TypeA); len(got) != 1 || !strings.HasPrefix(got[0], "198.18.") {
			t.Fatalf("logical/inverted/override projection failed for %s: %v", domain, got)
		}
	}
	for i := 0; i < 3; i++ {
		if real := query("udp", "::1", "foreign.test", dnsmessage.TypeA); len(real) != 1 || real[0] != "203.0.113.9" {
			t.Fatalf("direct device contaminated by FakeIP: %v", real)
		}
		if again := query("tcp", "127.0.0.1", "foreign.test", dnsmessage.TypeA); len(again) != 1 || again[0] != fake[0] {
			t.Fatalf("proxy device contaminated by direct cache: %v", again)
		}
	}
	for _, qt := range []dnsmessage.Type{dnsmessage.TypeAAAA, 64, 65, dnsmessage.TypeALL, dnsmessage.TypeTXT, dnsmessage.TypeMX} {
		if answer := query("udp", "127.0.0.1", "foreign.test", qt); len(answer) != 0 {
			t.Fatalf("unexpected proxy IPv6/address hints: %v", answer)
		}
	}
	stop()
	stop = start()
	defer stop()
	_ = query("udp", "127.0.0.1", "another.test", dnsmessage.TypeA)
	if again := query("udp", "127.0.0.1", "foreign.test", dnsmessage.TypeA); len(again) != 1 || again[0] != fake[0] {
		t.Fatalf("lost persistent mapping: %v -> %v", fake, again)
	}
}

func dnsExchange(network, address, domain string, qt dnsmessage.Type) ([]string, error) {
	name, err := dnsmessage.NewName(domain + ".")
	if err != nil {
		return nil, err
	}
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: 4123, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: qt, Class: dnsmessage.ClassINET}}}
	packet, err := m.Pack()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if network == "tcp" {
		packet = append([]byte{byte(len(packet) >> 8), byte(len(packet))}, packet...)
	}
	if _, err = conn.Write(packet); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	var n int
	if network == "tcp" {
		if _, err = io.ReadFull(conn, buf[:2]); err != nil {
			return nil, err
		}
		n = int(binary.BigEndian.Uint16(buf[:2]))
		if n > len(buf) {
			return nil, fmt.Errorf("oversized DNS")
		}
		_, err = io.ReadFull(conn, buf[:n])
	} else {
		n, err = conn.Read(buf)
	}
	if err != nil {
		return nil, err
	}
	var result dnsmessage.Message
	if err = result.Unpack(buf[:n]); err != nil {
		return nil, err
	}
	if result.RCode != dnsmessage.RCodeSuccess {
		return nil, fmt.Errorf("DNS rcode %v", result.RCode)
	}
	var answers []string
	for _, rr := range result.Answers {
		switch a := rr.Body.(type) {
		case *dnsmessage.AResource:
			answers = append(answers, net.IP(a.A[:]).String())
		case *dnsmessage.AAAAResource:
			answers = append(answers, net.IP(a.AAAA[:]).String())
		default:
			answers = append(answers, fmt.Sprint(rr.Header.Type))
		}
	}
	return answers, nil
}
