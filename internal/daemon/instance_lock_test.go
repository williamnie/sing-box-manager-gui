package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstanceLockRejectsSameDataDirAndReleases(t *testing.T) {
	dir := t.TempDir()
	first, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := AcquireInstanceLock(dir); err == nil {
		second.Close()
		t.Fatal("second manager acquired same data directory")
	}
	info, err := os.Stat(filepath.Join(dir, "manager.lock"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("lock record is not private")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal("could not reacquire released lock", err)
	}
	defer next.Close()
}

func TestInstanceLockResolvesAliasButAllowsDifferentDataDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "data")
	alias := filepath.Join(root, "alias")
	first, err := AcquireInstanceLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if second, err := AcquireInstanceLock(alias); err == nil {
		second.Close()
		t.Fatal("symlink alias bypassed lock")
	}
	independent, err := AcquireInstanceLock(filepath.Join(root, "different"))
	if err != nil {
		t.Fatal(err)
	}
	defer independent.Close()
}

func TestInstanceLockRefusesSymlinkFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "untouched")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "manager.lock")); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireInstanceLock(dir); err == nil {
		lock.Close()
		t.Fatal("followed symlink lock")
	}
	raw, _ := os.ReadFile(target)
	if string(raw) != "preserve" {
		t.Fatal("symlink destination was changed")
	}
}
