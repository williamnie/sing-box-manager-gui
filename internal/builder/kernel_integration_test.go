package builder

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 显式提供隔离下载的内核；只运行 check，绝不启动 TUN 或修改宿主网络。
func TestRealKernelGeneratedConfigurations(t *testing.T) {
	binary := os.Getenv("SBM_TEST_SINGBOX")
	if binary == "" {
		t.Skip("set SBM_TEST_SINGBOX for real kernel validation")
	}
	version, e := exec.Command(binary, "version").Output()
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"desktop", "linux-desktop", "gateway", "dns-bypass"} {
		t.Run(mode, func(t *testing.T) {
			s := storage.DefaultSettings()
			platform := "darwin"
			if mode == "linux-desktop" {
				platform = "linux"
			}
			if mode == "gateway" || mode == "dns-bypass" {
				s = gatewaySettings()
				platform = "linux"
				s.DeviceGroups = []storage.DeviceGroup{{ID: "strict", Policy: "strict"}, {ID: "bypass", Policy: "bypass"}}
				s.Devices = []storage.Device{{ID: "1", Name: "fixture", Enabled: true, Addresses: []string{"192.0.2.177"}, GroupID: "strict"}, {ID: "2", Name: "fixture-bypass", Enabled: true, Addresses: []string{"192.0.2.205"}, GroupID: "bypass"}}
			}
			if mode == "dns-bypass" {
				s = dnsBypassSettings()
				s.DeviceGroups = []storage.DeviceGroup{{ID: "direct", Policy: "direct"}}
				s.Devices = []storage.Device{{ID: "d", Enabled: true, GroupID: "direct", Addresses: []string{"192.0.2.205"}}}
			}
			s.AllowLAN = true
			s.Hosts = []storage.HostEntry{{ID: "h", Domain: "nas.lan", IPs: []string{"192.0.2.9"}, Enabled: true}}
			b := NewConfigBuilder(s, []storage.Node{{Tag: "test-proxy", Type: "socks", Server: "203.0.113.2", ServerPort: 1080}}, nil, []storage.Rule{{Name: "source-protocol-range", Enabled: true, RuleType: "port_range", Values: []string{"6881:60000"}, SourceCIDRs: []string{"192.0.2.205"}, Network: []string{"udp"}, Protocol: []string{"stun"}, Outbound: "DIRECT"}}, nil).WithPlatform(platform).WithSingBoxVersion(string(version))
			raw, e := b.BuildJSON()
			if e != nil {
				t.Fatal(e)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if e = os.WriteFile(path, []byte(raw), 0600); e != nil {
				t.Fatal(e)
			}
			action := "check"
			if (mode == "gateway" || mode == "dns-bypass") && runtime.GOOS != "linux" {
				action = "format"
				t.Log("非 Linux 主机只验证家庭模式配置解析；TUN/TProxy 完整 check 与数据面需 Linux")
			}
			cmd := exec.Command(binary, action, "-c", path)
			cmd.Dir = dir
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("real kernel rejected %s: %v\n%s", mode, e, out)
			}
		})
	}
}
