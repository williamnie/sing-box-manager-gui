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
