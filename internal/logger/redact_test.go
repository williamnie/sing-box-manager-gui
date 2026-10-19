package logger

import (
	"strings"
	"testing"
)

func TestRedactCredentialURLsAndFields(t *testing.T) {
	line := Redact(`request https://example.org/private/sub?token=secret password=do-not-log`)
	if strings.Contains(line, "secret") || strings.Contains(line, "do-not-log") || strings.Contains(line, "/private/sub") {
		t.Fatal(line)
	}
	for _, key := range []string{"auth_str", "Authorization", "private_key", "client_key", "pre_shared_key", "password", "username", "uuid"} {
		if !SensitiveKey(key) {
			t.Fatal(key)
		}
	}
	if SensitiveKey("server_port") {
		t.Fatal("non-secret field masked")
	}
}

// 快速路径必须与直接执行全部正则的结果一致。
func TestRedactFastPathMatchesRegexes(t *testing.T) {
	full := func(line string) string {
		line = terminalColor.ReplaceAllString(line, "")
		line = sensitiveURL.ReplaceAllString(line, "[URL REDACTED]")
		line = authorizationScheme.ReplaceAllString(line, "$1[REDACTED]")
		line = sensitivePair.ReplaceAllString(line, "$1$2[REDACTED]")
		return strings.ReplaceAll(line, "\x1b", "[ESC]")
	}
	for _, line := range []string{
		"+0800 2026-10-19 12:00:00 INFO [1 0ms] inbound/mixed[香港]: inbound connection from 192.0.2.1:1234",
		"\x1b[36mINFO\x1b[0m outbound/vless[a]: dial ss://user@host",
		"[ESC][31mERROR[ESC][0m Authorization: Bearer abc.def",
		"PASSWORD=hunter2 Token: xyz SECRET:\"s\"",
		"toKen=kelvin-sign-value",
		"paſsword=long-s-value",
		"plain line without anything",
	} {
		if got, want := Redact(line), full(line); got != want {
			t.Fatalf("Redact(%q) = %q, want %q", line, got, want)
		}
	}
}
