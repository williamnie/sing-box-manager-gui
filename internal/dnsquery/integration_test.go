package dnsquery

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// 真实内核只监听回环高端口，验证成功、拒绝、UDP 重用和 TCP 同连接多查询。
func TestRealKernelDNSQueryJournal(t *testing.T) {
	binaryPath := os.Getenv("SBM_TEST_SINGBOX")
	if binaryPath == "" {
		t.Skip("set SBM_TEST_SINGBOX for DNS journal integration")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "singbox.log")
	l, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	config := map[string]any{
		"log":       map[string]any{"level": "debug", "timestamp": true, "output": logPath},
		"dns":       map[string]any{"servers": []any{map[string]any{"type": "hosts", "tag": "local", "predefined": map[string]string{"ads.test": "203.0.113.8"}}}, "rules": []any{map[string]any{"domain": []string{"blocked.test"}, "action": "reject"}}, "final": "local"},
		"inbounds":  []any{map[string]any{"type": "direct", "tag": "lan-dns", "listen": "::", "listen_port": port}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "DIRECT"}},
		"route":     map[string]any{"rules": []any{map[string]any{"inbound": []string{"lan-dns"}, "action": "hijack-dns"}}},
	}
	raw, _ := json.Marshal(config)
	configPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binaryPath, "run", "-c", configPath)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() })
	var ready net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ready, err = net.DialTimeout("tcp", fmt.Sprintf("[::1]:%d", port), 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	ready.Close()
	j := New(filepath.Join(dir, "journal"))
	if err := j.Scan(logPath, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	exchange := func(conn net.Conn, domain string, id uint16) {
		t.Helper()
		conn.SetDeadline(time.Now().Add(time.Second))
		name, _ := dnsmessage.NewName(domain + ".")
		message := dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}
		packet, err := message.Pack()
		if err != nil {
			t.Fatal(err)
		}
		if conn.RemoteAddr().Network() == "tcp" {
			header := []byte{0, 0}
			binary.BigEndian.PutUint16(header, uint16(len(packet)))
			packet = append(header, packet...)
		}
		if _, err := conn.Write(packet); err != nil {
			t.Fatal(err)
		}
		if conn.RemoteAddr().Network() == "tcp" {
			var size uint16
			if err := binary.Read(conn, binary.BigEndian, &size); err != nil {
				t.Fatal(err)
			}
			if _, err := io.ReadFull(conn, make([]byte, size)); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := conn.Read(make([]byte, 4096)); err != nil {
				t.Fatal(err)
			}
		}
	}
	udp, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	exchange(udp, "ads.test", 1)
	exchange(udp, "blocked.test", 2)
	exchange(udp, "ads.test", 3)
	tcp, err := net.Dial("tcp", fmt.Sprintf("[::1]:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	exchange(tcp, "ads.test", 4)
	exchange(tcp, "blocked.test", 5)
	if err := j.Scan(logPath, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := j.Search(Filter{Limit: 50}, time.Now())
	if r.Total != 5 || len(r.Sources) != 2 {
		data, _ := os.ReadFile(logPath)
		t.Fatalf("queries lost or wrongly attributed: %+v\n%s", r, data)
	}
	if r = j.Search(Filter{Source: "127.0.0.1", Search: "blocked", Limit: 50}, time.Now()); r.Total != 1 {
		t.Fatalf("UDP rejected query lost: %+v", r)
	}
	if r = j.Search(Filter{Source: "::1", Search: "blocked", Limit: 50}, time.Now()); r.Total != 1 {
		t.Fatalf("TCP rejected query lost: %+v", r)
	}
}
