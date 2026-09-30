package logger

import (
	"strings"
	"testing"
)

func TestAuthorizationSchemeIsFullyRedacted(t *testing.T) {
	for _, line := range []string{`Authorization: Bearer abc-secret-value`, `{"Authorization":"Bearer abc-secret-value"}`, `Proxy-Authorization: Basic abc-secret-value`} {
		if out := Redact(line); strings.Contains(out, "abc-secret-value") {
			t.Fatalf("authorization credential remains: %q", out)
		}
	}
}

func TestRedactRemovesLogColorsBeforeLevelFiltering(t *testing.T) {
	for _, line := range []string{"2026-09-30 \x1b[36mINFO\x1b[0m inbound: test", "2026-09-30 [ESC][36mINFO[ESC][0m inbound: test"} {
		if out := Redact(line); out != "2026-09-30 INFO inbound: test" {
			t.Fatalf("ANSI prevents level matching: %q", out)
		}
	}
}
