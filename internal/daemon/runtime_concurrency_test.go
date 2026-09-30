package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func concurrentRuntimeFixture(t *testing.T) *ProcessManager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "applied.json")
	raw := []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:12345","secret":"private"}}}`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	pm := &ProcessManager{identity: processIdentity{PID: 123, Created: 456, Executable: "/fixture/core", Config: path, Directory: dir, ConfigHash: hex.EncodeToString(hash[:])}, configPath: path}
	pm.inspect = func(int) (processIdentity, error) { return pm.currentIdentity(), nil }
	return pm
}
func waitRuntimeSignal(t *testing.T, ch <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func TestRuntimeConcurrentReadLeasesAndStatusStayParallel(t *testing.T) {
	pm := concurrentRuntimeFixture(t)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	entered := make(chan struct{}, 3)
	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			done <- pm.WithRuntime(context.Background(), func(RuntimeTarget) error { entered <- struct{}{}; <-release; return nil })
		}()
	}
	for i := 0; i < 3; i++ {
		waitRuntimeSignal(t, entered, "read leases serialized or rejected")
	}
	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		if !pm.IsRunning() || pm.GetPID() != 123 {
			t.Error("status could not observe running fixture")
		}
	}()
	waitRuntimeSignal(t, statusDone, "IsRunning/GetPID blocked on an active read lease")
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { return nil }); err != nil {
		t.Fatalf("status blocked concurrent runtime request: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeLifecycleWriterWaitsForIssuedRequest(t *testing.T) {
	pm := concurrentRuntimeFixture(t)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	entered := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		readDone <- pm.WithRuntime(context.Background(), func(RuntimeTarget) error { close(entered); <-release; return nil })
	}()
	waitRuntimeSignal(t, entered, "request never acquired read lease")
	writerStarted := make(chan struct{})
	writerDone := make(chan struct{})
	newPath := filepath.Join(t.TempDir(), "draft.json")
	go func() { close(writerStarted); pm.SetConfigPath(newPath); close(writerDone) }()
	waitRuntimeSignal(t, writerStarted, "lifecycle writer not scheduled")
	select {
	case <-writerDone:
		t.Fatal("lifecycle changed target while a request was in flight")
	case <-time.After(40 * time.Millisecond):
	}
	// RWMutex 的等待写入者必须阻止新的运行租约插队。
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { t.Error("new request bypassed pending lifecycle writer"); return nil }); !errors.Is(err, ErrRuntimeBusy) {
		t.Fatalf("pending lifecycle did not reject new lease: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
	waitRuntimeSignal(t, writerDone, "lifecycle did not resume after request completed")
	pm.opMu.RLock()
	path := pm.configPath
	pm.opMu.RUnlock()
	if path != absolutePath(newPath) {
		t.Fatalf("lifecycle change lost: %s", path)
	}
}

func TestRuntimeReplacementChangesTokenAndUnknownConfigHashFailsClosed(t *testing.T) {
	pm := concurrentRuntimeFixture(t)
	old := ""
	if err := pm.WithRuntime(context.Background(), func(target RuntimeTarget) error { old = target.Instance; return nil }); err != nil {
		t.Fatal(err)
	}
	pm.opMu.Lock()
	pm.mu.Lock()
	pm.identity.Created++
	pm.mu.Unlock()
	pm.opMu.Unlock()
	stale := errors.New("stale queued operation")
	err := pm.WithRuntime(context.Background(), func(target RuntimeTarget) error {
		if target.Instance != old {
			return stale
		}
		t.Error("restarted process reused runtime token")
		return nil
	})
	if !errors.Is(err, stale) {
		t.Fatalf("queued old-instance request not rejected: %v", err)
	}
	pm.opMu.Lock()
	pm.mu.Lock()
	pm.identity.ConfigHash = ""
	pm.mu.Unlock()
	pm.opMu.Unlock()
	if err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { t.Error("untracked configuration reached callback"); return nil }); !errors.Is(err, ErrRuntimeChanged) {
		t.Fatalf("unknown hash accepted: %v", err)
	}
}

func TestRuntimeLeaseDetectsProcessReplacementBeforeReturning(t *testing.T) {
	pm := concurrentRuntimeFixture(t)
	err := pm.WithRuntime(context.Background(), func(RuntimeTarget) error { pm.mu.Lock(); pm.identity.Created++; pm.mu.Unlock(); return nil })
	if !errors.Is(err, ErrRuntimeChanged) {
		t.Fatalf("runtime exit did not detect process replacement: %v", err)
	}
}
