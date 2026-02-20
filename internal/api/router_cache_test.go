package api

import "testing"

func TestBuildRuleSetSignatureStableOrder(t *testing.T) {
	signature := buildRuleSetSignature(map[string]bool{
		"geosite-google": true,
		"geoip-cn":       true,
		"geosite-openai": true,
	})

	want := "geoip-cn,geosite-google,geosite-openai"
	if signature != want {
		t.Fatalf("buildRuleSetSignature() = %q, want %q", signature, want)
	}
}

func TestConfigCacheIncludesRuleSetSignature(t *testing.T) {
	s := &Server{}
	s.setCachedConfig(7, "geoip-cn,geosite-google", "cached-config")

	if got, ok := s.getCachedConfig(7, "geoip-cn,geosite-google"); !ok || got != "cached-config" {
		t.Fatalf("expected cache hit with exact signature, got ok=%v, config=%q", ok, got)
	}

	if got, ok := s.getCachedConfig(7, "geoip-cn"); ok || got != "" {
		t.Fatalf("expected cache miss with different signature, got ok=%v, config=%q", ok, got)
	}

	if got, ok := s.getCachedConfig(8, "geoip-cn,geosite-google"); ok || got != "" {
		t.Fatalf("expected cache miss with different version, got ok=%v, config=%q", ok, got)
	}
}
