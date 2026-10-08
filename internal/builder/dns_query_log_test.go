package builder

import (
	"github.com/xiaobei/singbox-manager/internal/storage"
	"testing"
)

func TestDNSQueryLoggingNeedsDetailedEventsAndRestoresLevel(t *testing.T) {
	s := storage.DefaultSettings()
	s.LogLevel = "warn"
	b := NewConfigBuilder(s, nil, nil, nil, nil)
	if b.buildLog().Level != "warn" {
		t.Fatal("normal level changed")
	}
	s.DNSQueryLogEnabled = true
	if b.buildLog().Level != "debug" {
		t.Fatal("DNS queries would be silently absent")
	}
	s.LogLevel = "trace"
	if b.buildLog().Level != "trace" {
		t.Fatal("trace was downgraded")
	}
	s.LogLevel = "warn"
	s.DNSQueryLogEnabled = false
	if b.buildLog().Level != "warn" {
		t.Fatal("disabling did not restore chosen level")
	}
}
