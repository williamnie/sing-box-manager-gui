package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeUsesRunningIdentityNotDraftPath(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:12345","secret":"private"}}}`)
	path := filepath.Join(dir, "applied.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	id := processIdentity{PID: 123, Created: 456, Config: path, Directory: dir, Executable: "/fixture/core", ConfigHash: hex.EncodeToString(hash[:])}
	pm := &ProcessManager{identity: id, configPath: filepath.Join(dir, "draft.json"), inspect: func(int) (processIdentity, error) { return id, nil }}
	called := false
	err := pm.WithRuntime(context.Background(), func(target RuntimeTarget) error {
		called = true
		if target.Controller != "127.0.0.1:12345" || target.Secret != "private" || target.Instance == "" {
			t.Fatalf("wrong runtime target: controller=%s", target.Controller)
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	if err := os.WriteFile(path, []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:12346"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { t.Fatal("changed config accepted"); return nil }); !errors.Is(err, ErrRuntimeChanged) {
		t.Fatalf("want changed, got %v", err)
	}
}

func TestRuntimeFailsClosedDuringLifecycleAndOnUntrackedConfig(t *testing.T) {
	pm := &ProcessManager{}
	pm.opMu.Lock()
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { return nil }); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("want busy: %v", err)
	}
	pm.opMu.Unlock()
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { return nil }); !errors.Is(err, ErrRuntimeStopped) {
		t.Fatalf("want stopped: %v", err)
	}
	pm.identity = processIdentity{PID: 123, Created: 456}
	pm.inspect = func(int) (processIdentity, error) { return pm.identity, nil }
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { return nil }); !errors.Is(err, ErrRuntimeChanged) {
		t.Fatalf("untracked config accepted: %v", err)
	}
}
