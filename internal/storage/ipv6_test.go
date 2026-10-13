package storage

import (
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/gateway"
)

func TestAutomaticIPv6RejectsEnabledFixedDeviceAddresses(t *testing.T) {
	s := &Settings{DeploymentRole: "gateway", SingBoxPath: "bin/sing-box", ConfigPath: "generated/config.json", MixedPort: 8888, WebPort: 9090, ClashAPIPort: 9091,
		Gateway:      gateway.Config{IPv6Mode: "proxy", IPv6LANSource: "auto", LANCIDRs: []string{"192.0.2.0/24"}},
		DeviceGroups: []DeviceGroup{{ID: "split", Policy: "split"}},
		Devices:      []Device{{ID: "client", Name: "client", Addresses: []string{"2001:db8::123"}, GroupID: "split", Enabled: true}},
	}
	if e := ValidateSettings(s); e == nil || !strings.Contains(e.Error(), "固定 IPv6") {
		t.Fatalf("fixed IPv6 device accepted: %v", e)
	}
	s.Devices[0].Enabled = false
	if e := ValidateSettings(s); e != nil {
		t.Fatal("disabled device history should be preserved", e)
	}
	s.Devices[0].Enabled = true
	s.Devices[0].Addresses = []string{"192.0.2.205"}
	if e := ValidateSettings(s); e != nil {
		t.Fatal("IPv4 device policy regressed", e)
	}
}
