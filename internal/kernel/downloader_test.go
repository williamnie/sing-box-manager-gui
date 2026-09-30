package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMissingCandidatePreservesInstalledKernel(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil)
	if err := os.MkdirAll(filepath.Dir(m.binPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.binPath, []byte("working-kernel"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.installBinary(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected missing candidate error")
	}
	got, err := os.ReadFile(m.binPath)
	if err != nil || string(got) != "working-kernel" {
		t.Fatalf("installed kernel was lost: %q, %v", got, err)
	}
}

func TestReplaceBinaryRetainsBackupAndCanRollback(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sing-box")
	candidate := filepath.Join(dir, "candidate")
	if err := os.WriteFile(target, []byte("old-kernel"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("new-kernel"), 0755); err != nil {
		t.Fatal(err)
	}
	rollback, err := ReplaceBinary(candidate, target)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	backup, _ := os.ReadFile(target + ".previous")
	if string(got) != "new-kernel" || string(backup) != "old-kernel" {
		t.Fatalf("bad replacement: %s / %s", got, backup)
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(target)
	if string(got) != "old-kernel" {
		t.Fatal("rollback did not restore old binary")
	}
}

func TestCapabilitiesVersionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		version                  string
		redirect, typed, gateway bool
	}{
		{"1.8.0", false, false, false}, {"1.10.0", true, false, false}, {"1.12.0", true, true, false}, {"1.14.1", true, true, true}, {"sing-box version 1.15.0-alpha.1", true, true, true},
	} {
		got, err := CapabilitiesFor(tc.version)
		if err != nil {
			t.Fatal(err)
		}
		if got.AutoRedirect != tc.redirect || got.TypedDNS != tc.typed || got.Gateway != tc.gateway {
			t.Fatalf("wrong capabilities for %s: %+v", tc.version, got)
		}
	}
	for _, version := range []string{"garbage", "1.7.0", "2.0.0", "1.14"} {
		if _, err := CapabilitiesFor(version); err == nil {
			t.Fatalf("accepted unsupported version %s", version)
		}
	}
}

func TestInstallHookFailureLeavesExistingBinary(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, nil)
	if err := os.MkdirAll(filepath.Dir(m.binPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.binPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, "candidate")
	if err := os.WriteFile(candidate, []byte("#!/bin/sh\nprintf 'sing-box version 1.14.1\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	called := false
	m.SetInstallHook(func(path string) error { called = path == candidate; return fmt.Errorf("health check failed") })
	if err := m.installBinary(candidate); err == nil || !called {
		t.Fatalf("hook was not used: %v", err)
	}
	raw, _ := os.ReadFile(m.binPath)
	if string(raw) != "old" {
		t.Fatal("failed hook overwrote old kernel")
	}
}

func TestProgressSnapshotCannotMutateManager(t *testing.T) {
	m := NewManager(t.TempDir(), nil)
	progress := m.GetProgress()
	progress.Status = "corrupt"
	if m.GetProgress().Status != "idle" {
		t.Fatal("progress exposed mutable shared state")
	}
}
