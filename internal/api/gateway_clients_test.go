package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/gateway"
	"github.com/xiaobei/singbox-manager/internal/runtimecontrol"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

func TestGatewayClientsUseRuntimeIPv6WithoutMutatingSettings(t *testing.T) {
	s, _, g := prepareGateway(t)
	settings := s.store.GetSettings()
	settings.Gateway.IPv6Mode, settings.Gateway.IPv6LANSource = "proxy", "auto"
	settings.Gateway.Enabled = true
	plan, err := gateway.Preview("gateway", settings.Gateway)
	if err != nil {
		t.Fatal(err)
	}
	g.applied, g.digest = true, plan.ConfigDigest
	g.ipv6 = &gateway.IPv6Status{Prefixes: []string{"2001:db8:2::/64"}, Generation: 2}
	before, _ := json.Marshal(settings)
	observed := s.gatewayClientSettings(context.Background(), settings)
	s.gatewayClientSettings(context.Background(), settings)
	if g.statusCalls.Load() != 1 {
		t.Fatal("client observation repeatedly captured gateway resources")
	}
	if _, ok := gatewayClientAddress("2001:db8:2::205", observed); !ok {
		t.Fatal("new IPv6 client hidden")
	}
	if _, ok := gatewayClientAddress("2001:db8:1::205", observed); ok {
		t.Fatal("stale prefix accepted")
	}
	after, _ := json.Marshal(settings)
	if string(before) != string(after) {
		t.Fatal("runtime prefixes mutated stored configuration")
	}
	g.digest = "foreign-config"
	s.observedClients.ipv6CheckedAt = time.Now().Add(-time.Minute)
	if got := s.gatewayClientSettings(context.Background(), settings); got != settings {
		t.Fatal("foreign helper topology was trusted")
	}
}

func TestGatewayClientsDiscoverUnregisteredDNSClientWithoutChangingPolicy(t *testing.T) {
	s, _, _ := prepareGateway(t)
	cookie := setup(t, s)
	settings := s.store.GetSettings()
	before, _ := json.Marshal(settings)
	path := filepath.Join(s.store.GetDataDir(), "logs", "singbox.log")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Format("-0700 2006-01-02 15:04:05")
	log := stamp + " INFO [123 0ms] inbound/direct[lan-dns]: inbound packet connection from 192.0.2.84:23456\n"
	log += stamp + " INFO [124 0ms] inbound/mixed[mixed-in]: inbound connection from 127.0.0.1:23456\n"
	log += stamp + " INFO [125 0ms] inbound/direct[lan-dns]: inbound packet connection from 203.0.113.5:23456\n"
	if err := os.WriteFile(path, []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	if response := request(s, "GET", "/api/gateway/clients", nil, nil, ""); response.Code != 401 {
		t.Fatal("device observations require authentication", response.Code)
	}
	response := request(s, "GET", "/api/gateway/clients", nil, cookie, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			Clients []struct {
				Address    string `json:"address"`
				Configured bool   `json:"configured"`
				DNSSeen    bool   `json:"dns_seen"`
				Policy     string `json:"policy"`
			} `json:"clients"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	clients := body.Data.Clients
	if len(clients) != 1 || clients[0].Address != "192.0.2.84" || clients[0].Configured || !clients[0].DNSSeen || clients[0].Policy != "split" {
		t.Fatalf("unregistered client missing or wrong policy: %+v", clients)
	}
	after, _ := json.Marshal(s.store.GetSettings())
	if string(before) != string(after) {
		t.Fatal("observation changed persistent device policy")
	}
}

func TestGatewayClientHistoryBoundsAndPolicyChanges(t *testing.T) {
	s, _, _ := prepareGateway(t)
	settings := s.store.GetSettings()
	settings.Gateway.LANCIDRs = append(settings.Gateway.LANCIDRs, "2001:db8::/64")
	settings.DeviceGroups = []storage.DeviceGroup{{ID: "direct", Policy: "direct"}}
	settings.Devices = []storage.Device{{ID: "test", Name: "设备", Addresses: []string{"192.0.2.80/28"}, GroupID: "direct", Enabled: true}}
	now := time.Now()
	tracker := gatewayClientTracker{}
	tracker.observe("192.0.2.83", now.Add(-25*time.Hour), true, false)
	tracker.observe("192.0.2.84", now.Add(-time.Minute), true, false)
	clients := tracker.snapshot(settings, []runtimecontrol.Connection{{Source: "192.0.2.84"}, {Source: "127.0.0.1"}}, now)
	if len(clients) != 1 || clients[0].ActiveConnections != 1 || !clients[0].DNSSeen || !clients[0].ProxySeen || clients[0].Policy != "direct" {
		t.Fatalf("history or active source merged incorrectly: %+v", clients)
	}
	settings.Devices = nil
	clients = tracker.snapshot(settings, nil, now)
	if clients[0].Configured || clients[0].Policy != "split" || clients[0].Name != "" || clients[0].ActiveConnections != 0 {
		t.Fatal("removed device policy remained in observed history", clients)
	}
	for i := 1; i <= maxObservedGatewayClients+50; i++ {
		tracker.observe(fmt.Sprintf("2001:db8::%x", i), now.Add(time.Duration(i)*time.Millisecond), true, false)
	}
	if len(tracker.clients) != maxObservedGatewayClients {
		t.Fatal("unbounded client observations", len(tracker.clients))
	}
	if clients := tracker.snapshot(settings, nil, now.Add(25*time.Hour)); len(clients) != 0 {
		t.Fatal("expired observations retained", clients)
	}
}
