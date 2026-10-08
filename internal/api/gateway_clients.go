package api

import (
	"context"
	"net/netip"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"github.com/xiaobei/singbox-manager/internal/runtimecontrol"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

const maxObservedGatewayClients = 512

type observedGatewayClient struct {
	Address           string `json:"address"`
	Name              string `json:"name"`
	Configured        bool   `json:"configured"`
	Policy            string `json:"policy"`
	LastSeenAt        int64  `json:"last_seen_at"`
	DNSSeen           bool   `json:"dns_seen"`
	ProxySeen         bool   `json:"proxy_seen"`
	ActiveConnections int    `json:"active_connections"`
}

// 只保存有界的来源观察，不写入设备策略；未登记的客户端仍然正常分流。
type gatewayClientTracker struct {
	mu       sync.Mutex
	cursor   string
	lastRead time.Time
	clients  map[string]observedGatewayClient
	cancel   context.CancelFunc
	done     chan struct{}
}

func (s *Server) startGatewayClientObservation() {
	t := &s.observedClients
	t.mu.Lock()
	if t.cancel != nil {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	t.cancel, t.done = cancel, done
	t.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			settings := s.store.GetSettings()
			// 采集错误在查询页展示，不阻断 DNS 或反复写入正在被采集的日志。
			_ = s.dnsQueries.Scan(filepath.Join(s.store.GetDataDir(), "logs", "singbox.log"), settings.DNSQueryLogEnabled, time.Now())
			if s.platform == "linux" && settings.DeploymentRole == "gateway" {
				t.scan(filepath.Join(s.store.GetDataDir(), "logs", "singbox.log"), settings, time.Now())
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Server) stopGatewayClientObservation() {
	t := &s.observedClients
	t.mu.Lock()
	cancel, done := t.cancel, t.done
	t.cancel, t.done = nil, nil
	t.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

var inboundClientLog = regexp.MustCompile(`^([+-]\d{4} \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \S+ \[[^\]]+\] inbound/[^\[]+\[([^\]]+)\]: inbound (?:packet )?connection from (\S+)$`)

func gatewayClientAddress(value string, settings *storage.Settings) (netip.Addr, bool) {
	address, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	if address.IsLoopback() || address.IsUnspecified() || address.IsMulticast() || address.String() == settings.Gateway.LANAddress {
		return netip.Addr{}, false
	}
	for _, cidr := range settings.Gateway.LANCIDRs {
		if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Contains(address) {
			return address, true
		}
	}
	return netip.Addr{}, false
}

func (t *gatewayClientTracker) observe(address string, at time.Time, dns, proxy bool) {
	if t.clients == nil {
		t.clients = make(map[string]observedGatewayClient)
	}
	client := t.clients[address]
	client.Address = address
	if client.LastSeenAt < at.UnixMilli() {
		client.LastSeenAt = at.UnixMilli()
	}
	client.DNSSeen = client.DNSSeen || dns
	client.ProxySeen = client.ProxySeen || proxy
	t.clients[address] = client
	if len(t.clients) > maxObservedGatewayClients {
		oldest := address
		for key, candidate := range t.clients {
			if candidate.LastSeenAt < t.clients[oldest].LastSeenAt {
				oldest = key
			}
		}
		delete(t.clients, oldest)
	}
}

func (t *gatewayClientTracker) scan(path string, settings *storage.Settings, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now.Sub(t.lastRead) < time.Second {
		return true
	}
	t.lastRead = now
	// 首次只读取日志尾部；后续按游标增量读取，单次最多 1 MiB。
	for batchIndex := 0; batchIndex < 4; batchIndex++ {
		batch, err := logger.ReadLogBatch(path, t.cursor, logger.MaxLogBatchLines)
		if err != nil {
			return false
		}
		t.cursor = batch.Cursor
		for _, line := range batch.Lines {
			parts := inboundClientLog.FindStringSubmatch(line)
			if len(parts) != 4 {
				continue
			}
			at, err := time.Parse("-0700 2006-01-02 15:04:05", parts[1])
			if err != nil || now.Sub(at) > 24*time.Hour || at.After(now.Add(time.Minute)) {
				continue
			}
			endpoint, err := netip.ParseAddrPort(parts[3])
			if err != nil {
				continue
			}
			address, ok := gatewayClientAddress(endpoint.Addr().String(), settings)
			if ok {
				t.observe(address.String(), at, parts[2] == "lan-dns", parts[2] != "lan-dns")
			}
		}
		if !batch.More {
			break
		}
	}
	return true
}

func (t *gatewayClientTracker) snapshot(settings *storage.Settings, connections []runtimecontrol.Connection, now time.Time) []observedGatewayClient {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := make(map[string]int)
	for _, connection := range connections {
		if address, ok := gatewayClientAddress(connection.Source, settings); ok {
			active[address.String()]++
			t.observe(address.String(), now, false, true)
		}
	}
	result := make([]observedGatewayClient, 0, len(t.clients))
	devices := storage.EffectiveDevices(settings)
	for key, client := range t.clients {
		if _, ok := gatewayClientAddress(key, settings); !ok || now.UnixMilli()-client.LastSeenAt > int64(24*time.Hour/time.Millisecond) {
			delete(t.clients, key)
			continue
		}
		client.ActiveConnections = active[key]
		client.Policy = "split"
		address, _ := netip.ParseAddr(key)
		for _, device := range devices {
			if !device.Enabled {
				continue
			}
			matches := false
			for _, value := range device.Addresses {
				if prefix, err := storage.SourcePrefix(value); err == nil && prefix.Contains(address) {
					matches = true
				}
			}
			if !matches {
				continue
			}
			client.Name, client.Configured = device.Name, true
			for _, group := range settings.DeviceGroups {
				if group.ID == device.GroupID {
					client.Policy = group.Policy
				}
			}
			break
		}
		result = append(result, client)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LastSeenAt != result[j].LastSeenAt {
			return result[i].LastSeenAt > result[j].LastSeenAt
		}
		return result[i].Address < result[j].Address
	})
	return result
}

func (s *Server) gatewayClients(c *gin.Context) {
	settings := s.store.GetSettings()
	if s.platform != "linux" || settings.DeploymentRole != "gateway" {
		c.JSON(200, gin.H{"data": gin.H{"clients": []observedGatewayClient{}, "runtime_available": false, "logs_available": false}})
		return
	}
	now := time.Now()
	logsAvailable := s.observedClients.scan(filepath.Join(s.store.GetDataDir(), "logs", "singbox.log"), settings, now)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var connections []runtimecontrol.Connection
	err := s.withRuntime(ctx, "", func(client *runtimecontrol.Client, _ string) error {
		snapshot, err := client.Connections(ctx)
		connections = snapshot.Connections
		return err
	})
	c.JSON(200, gin.H{"data": gin.H{"clients": s.observedClients.snapshot(settings, connections, now), "runtime_available": err == nil, "logs_available": logsAvailable}})
}
