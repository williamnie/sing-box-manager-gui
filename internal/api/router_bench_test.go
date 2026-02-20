package api

import "testing"

func BenchmarkNeedHardRestartNoChange(b *testing.B) {
	s := &Server{}
	config := []byte(`{"inbounds":[{"type":"mixed","listen_port":2080}],"experimental":{"cache_file":{"enabled":true}},"log":{"level":"info"}}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if s.needHardRestart(config, config) {
			b.Fatalf("expected no hard restart when config unchanged")
		}
	}
}

func BenchmarkNeedHardRestartColdStart(b *testing.B) {
	s := &Server{}
	oldConfig := []byte{}
	newConfig := []byte(`{"inbounds":[{"type":"mixed","listen_port":3080}],"experimental":{"cache_file":{"enabled":true}},"log":{"level":"info"}}`)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if !s.needHardRestart(oldConfig, newConfig) {
			b.Fatalf("expected hard restart when old config is empty")
		}
	}
}
