package gateway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// 包只注入新建的网络 namespace，绝不使用宿主接口或 systemd。
func TestAutoIPv6LinuxNamespace(t *testing.T) {
	if os.Getenv("SBM_GATEWAY_NETNS_TEST") != "1" {
		t.Skip("requires explicit isolated Linux network test")
	}
	if os.Geteuid() != 0 {
		t.Fatal("requires root in an isolated namespace")
	}
	ns, e := os.Readlink("/proc/self/ns/net")
	if e != nil {
		t.Fatal(e)
	}
	parent := os.Getenv("SBM_GATEWAY_PARENT_NETNS")
	if parent == "" {
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		unshare := trustedExecutable("unshare")
		if unshare == "" {
			t.Fatal("unshare unavailable")
		}
		cmd := exec.Command(unshare, "--net", "--", exe, "-test.run=^TestAutoIPv6LinuxNamespace$", "-test.v")
		cmd.Env = append(os.Environ(), "SBM_GATEWAY_PARENT_NETNS="+ns)
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("namespace child: %v\n%s", e, out)
		}
		t.Log(string(out))
		return
	}
	if ns == parent {
		t.Fatal("refusing parent namespace")
	}
	runner, e := NewSystemRunner()
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	run := func(name string, args ...string) string {
		t.Helper()
		out, e := runner.Run(ctx, name, args, "")
		if e != nil {
			t.Fatalf("%s %v: %s %v", name, args, out, e)
		}
		return out
	}
	for _, args := range [][]string{
		{"link", "set", "lo", "up"},
		{"link", "add", "eth0", "type", "veth", "peer", "name", "client0"},
		{"link", "set", "eth0", "up"}, {"link", "set", "client0", "up"},
		{"address", "add", "192.0.2.142/24", "dev", "eth0"},
		{"address", "add", "2001:db8:1::142/64", "dev", "eth0", "nodad", "valid_lft", "600", "preferred_lft", "600"},
		{"address", "add", "fe80::142/64", "dev", "eth0", "nodad"},
		{"route", "add", "default", "via", "192.0.2.1", "dev", "eth0"},
		{"-6", "route", "add", "default", "via", "fe80::1", "dev", "eth0"},
		{"-6", "neigh", "add", "fe80::1", "lladdr", "02:00:00:00:00:01", "nud", "permanent", "dev", "eth0"},
		{"-6", "neigh", "add", "2001:db8:1::205", "lladdr", "02:00:00:00:00:02", "nud", "permanent", "dev", "eth0"},
		{"link", "add", TUNInterface, "type", "dummy"}, {"link", "set", TUNInterface, "up"},
	} {
		run("ip", args...)
	}
	c := Normalize(Config{Enabled: true, LANInterface: "eth0", LANAddress: "192.0.2.142", LANCIDRs: []string{"192.0.2.0/24"}, UpstreamGateway: "192.0.2.1", IPv6Mode: "proxy", IPv6LANSource: "auto"})
	m := NewManager(namespaceRunner{runner}, Policy{AllowGateway: true, AllowedLANInterfaces: []string{"eth0"}, AllowedUplinkInterfaces: []string{"eth0"}, AllowedLANCIDRs: c.LANCIDRs, DynamicIPv6Interfaces: []string{"eth0"}}, t.TempDir())
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	status, e := m.Status(ctx)
	if e != nil || status.Drift || len(status.IPv6.Prefixes) != 1 {
		t.Fatalf("initial status %+v ipv6=%+v %v; addresses=%s routes=%s", status, status.IPv6, e, run("ip", "-j", "address", "show", "dev", "eth0"), run("ip", "-j", "-6", "route", "show", "table", "main", "dev", "eth0"))
	}
	originalDigest := status.ConfigDigest
	// 停核状态（没有运行 sing-box），新旧、ULA 和未知来源都必须命中兜底拒绝。
	for _, source := range []string{"2001:db8:1::205", "2001:db8:2::205", "fd00::205"} {
		before := nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
		injectIPv6(t, "client0", "eth0", source, "2001:db8:ffff::1")
		waitForPackets(t, func() uint64 {
			return nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
		}, before+1)
	}
	// 单网卡的 Redirect 在 FORWARD 检查前生成，也必须被输出链阻止。
	if nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "ipv6_redirect")) == 0 {
		t.Fatal("single-arm test did not exercise Redirect suppression")
	}
	// 以模拟 TUN 路由验证保护没有拒绝发往受管 TUN 的包。
	run("ip", "-6", "route", "add", "2001:db8:99::/64", "dev", TUNInterface)
	before := nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	injectIPv6(t, "client0", "eth0", "2001:db8:2::205", "2001:db8:99::1")
	time.Sleep(40 * time.Millisecond)
	if nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward")) != before {
		t.Fatal("TUN traffic rejected")
	}
	// 地址替换经过重叠期；弃用但仍有效的旧地址不能立即丢失 LAN 例外。
	run("ip", "address", "change", "2001:db8:1::142/64", "dev", "eth0", "preferred_lft", "0", "valid_lft", "600")
	run("ip", "address", "add", "2001:db8:2::142/64", "dev", "eth0", "nodad", "preferred_lft", "600", "valid_lft", "600")
	if e := m.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	status, e = m.Status(ctx)
	if e != nil || status.Drift || len(status.IPv6.Prefixes) != 2 || status.ConfigDigest != originalDigest {
		t.Fatalf("overlap failed: %+v %v", status, e)
	}
	run("ip", "address", "del", "2001:db8:1::142/64", "dev", "eth0")
	if e := m.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	status, e = m.Status(ctx)
	if e != nil || status.Drift || len(status.IPv6.Prefixes) != 1 || status.IPv6.Prefixes[0] != "2001:db8:2::/64" {
		t.Fatalf("rotation failed: %+v %v", status, e)
	}
	run("ip", "address", "del", "2001:db8:2::142/64", "dev", "eth0")
	if e := m.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	status, e = m.Status(ctx)
	if e != nil || status.Drift || len(status.IPv6.Prefixes) != 0 {
		t.Fatalf("zero-prefix state: %+v %v", status, e)
	}
	before = nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	injectIPv6(t, "client0", "eth0", "2001:db8:3::205", "2001:db8:ffff::2")
	waitForPackets(t, func() uint64 {
		return nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	}, before+1)
	// helper 同一次启动重建后仍可精确识别已更新的表，恢复全部自有资源。
	restarted := NewManager(namespaceRunner{runner}, m.Policy, m.StateDir)
	if e := restarted.SyncIPv6(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := restarted.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(run("nft", "list", "tables"), TableName) {
		t.Fatal("rollback left IPv6 sets/redirect guard")
	}
	// 双网卡：上联可达并不意味着允许绕过 TUN。
	for _, args := range [][]string{
		{"link", "add", "eth1", "type", "dummy"}, {"link", "set", "eth1", "up"},
		{"address", "add", "192.0.3.142/24", "dev", "eth1"},
		{"address", "add", "2001:db8:3::142/64", "dev", "eth0", "nodad"},
		{"route", "replace", "default", "via", "192.0.3.1", "dev", "eth1"},
		{"-6", "route", "replace", "default", "via", "fe80::1", "dev", "eth1"},
	} {
		run("ip", args...)
	}
	c.UplinkInterface, c.UpstreamGateway = "eth1", "192.0.3.1"
	m.Policy.AllowedUplinkInterfaces = []string{"eth1"}
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	before = nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	injectIPv6(t, "client0", "eth0", "2001:db8:4::205", "2001:db8:ffff::3")
	waitForPackets(t, func() uint64 {
		return nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	}, before+1)
	// 即使目的地址属于学到的 LAN 前缀，更具体的 WAN 路由也不能获得 LAN 例外。
	run("ip", "-6", "route", "add", "2001:db8:3::99/128", "via", "fe80::1", "dev", "eth1")
	before = nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	injectIPv6(t, "client0", "eth0", "2001:db8:4::205", "2001:db8:3::99")
	waitForPackets(t, func() uint64 {
		return nftPackets(t, run("nft", "-j", "list", "chain", "inet", TableName, "lan_forward"))
	}, before+1)
	if _, e := m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	t.Log("real nft apply/renumber/empty set, single/dual-arm packet rejection, Redirect suppression, TUN exception, restart and rollback verified")
}

func nftPackets(t *testing.T, raw string) uint64 {
	t.Helper()
	var v any
	if e := json.Unmarshal([]byte(raw), &v); e != nil {
		t.Fatal(e)
	}
	var visit func(any) uint64
	visit = func(v any) uint64 {
		var n uint64
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "counter" {
					if c, ok := v.(map[string]any); ok {
						if p, ok := c["packets"].(float64); ok {
							n += uint64(p)
						}
					}
				} else {
					n += visit(v)
				}
			}
		case []any:
			for _, v := range x {
				n += visit(v)
			}
		}
		return n
	}
	return visit(v)
}

func waitForPackets(t *testing.T, read func() uint64, want uint64) {
	t.Helper()
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if read() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("injected IPv6 packet did not reach expected firewall counter")
}

func injectIPv6(t *testing.T, from, to, source, destination string) {
	t.Helper()
	client, e := net.InterfaceByName(from)
	if e != nil {
		t.Fatal(e)
	}
	lan, e := net.InterfaceByName(to)
	if e != nil {
		t.Fatal(e)
	}
	frame := make([]byte, 14+40+8)
	copy(frame[:6], lan.HardwareAddr)
	copy(frame[6:12], client.HardwareAddr)
	binary.BigEndian.PutUint16(frame[12:14], unix.ETH_P_IPV6)
	frame[14] = 0x60
	binary.BigEndian.PutUint16(frame[18:20], 8)
	frame[20] = 17
	frame[21] = 64
	src, dst := netip.MustParseAddr(source).As16(), netip.MustParseAddr(destination).As16()
	copy(frame[22:38], src[:])
	copy(frame[38:54], dst[:])
	binary.BigEndian.PutUint16(frame[54:56], 23456)
	binary.BigEndian.PutUint16(frame[56:58], 443)
	binary.BigEndian.PutUint16(frame[58:60], 8)
	// UDP 校验和包括 IPv6 伪首部。
	var sum uint32
	for i := 22; i < 54; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(frame[i : i+2]))
	}
	sum += 8 + 17
	for i := 54; i < 60; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(frame[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	checksum := ^uint16(sum)
	if checksum == 0 {
		checksum = 0xffff
	}
	binary.BigEndian.PutUint16(frame[60:62], checksum)
	proto := (unix.ETH_P_IPV6&0xff)<<8 | unix.ETH_P_IPV6>>8
	fd, e := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, proto)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(fd)
	if e = unix.Sendto(fd, frame, 0, &unix.SockaddrLinklayer{Ifindex: client.Index, Protocol: uint16(proto), Halen: 6}); e != nil {
		t.Fatal(e)
	}
}
