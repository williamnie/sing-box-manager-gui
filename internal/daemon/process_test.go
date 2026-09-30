package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestRecoveryRejectsUnrelatedPID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "singbox.pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	pm := NewProcessManager(filepath.Join(dir, "sing-box"), filepath.Join(dir, "config.json"), dir)
	if pm.IsRunning() {
		t.Fatal("unrelated process from stale PID file was adopted")
	}
}

// TestMain 同一个测试二进制可充当隔离的 sing-box 子进程，不访问实际网络配置。
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "--manager-fixture" {
		exe, _ := os.Executable()
		pm := NewProcessManager(exe, filepath.Join(os.Args[2], "config.json"), os.Args[2])
		pm.healthWindow = 60 * time.Millisecond
		if err := pm.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) == 4 && os.Args[1] == "--internal-log-relay" {
		if err := RunLogRelay(os.Args[2], os.Args[3]); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("sing-box version 1.14.1")
		os.Exit(0)
	}
	if len(os.Args) == 4 && (os.Args[1] == "run" || os.Args[1] == "check") && os.Args[2] == "-c" {
		raw, err := os.ReadFile(os.Args[3])
		if err != nil {
			os.Exit(2)
		}
		var cfg map[string]any
		if json.Unmarshal(raw, &cfg) != nil || cfg["reject"] == true {
			fmt.Fprintln(os.Stderr, "invalid config")
			os.Exit(3)
		}
		if os.Args[1] == "check" {
			os.Exit(0)
		}
		if cfg["crash"] == true {
			time.Sleep(20 * time.Millisecond)
			os.Exit(4)
		}
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, os.Interrupt)
		if cfg["emit_log"] == true {
			ticker := time.NewTicker(20 * time.Millisecond)
			for {
				select {
				case <-ch:
					os.Exit(0)
				case <-ticker.C:
					fmt.Fprintln(os.Stderr, "tick", cfg["password"])
				}
			}
		}
		<-ch
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func newHelperManager(t *testing.T) *ProcessManager {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pm := NewProcessManager(exe, filepath.Join(dir, "config.json"), dir)
	pm.healthWindow = 60 * time.Millisecond
	pm.healthTimeout = 250 * time.Millisecond
	if err := os.WriteFile(pm.configPath, []byte(`{"marker":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pm.Stop(); err != nil {
			t.Error(err)
		}
	})
	return pm
}

func TestApplyCheckFailurePreservesFileAndRunningProcess(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	pid := pm.GetPID()
	if err := pm.ApplyConfig([]byte(`{"reject":true}`)); err == nil {
		t.Fatal("expected failed validation")
	}
	raw, _ := os.ReadFile(pm.configPath)
	if string(raw) != `{"marker":"old"}` || !pm.IsRunning() || pm.GetPID() != pid {
		t.Fatalf("validation changed live instance: %s", raw)
	}
}

func TestApplyRollsBackFailedStartupAndRestartsOldConfig(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	if err := pm.ApplyConfig([]byte(`{"crash":true}`)); err == nil {
		t.Fatal("expected failed health check")
	}
	raw, _ := os.ReadFile(pm.configPath)
	backup, _ := os.ReadFile(pm.configPath + ".previous")
	if string(raw) != `{"marker":"old"}` || string(backup) != string(raw) || !pm.IsRunning() {
		t.Fatalf("rollback incomplete: current=%s backup=%s running=%v", raw, backup, pm.IsRunning())
	}
}

func TestApplyStoppedIsAtomicPrivateAndDoesNotStart(t *testing.T) {
	pm := newHelperManager(t)
	candidate := []byte(`{"marker":"new","password":"very-secret"}`)
	if err := pm.CheckConfig(candidate); err != nil {
		t.Fatal(err)
	}
	old, _ := os.ReadFile(pm.configPath)
	if string(old) != `{"marker":"old"}` {
		t.Fatal("preview changed config")
	}
	if err := pm.ApplyConfig(candidate); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(pm.configPath)
	info, _ := os.Stat(pm.configPath)
	if string(raw) != string(candidate) || info.Mode().Perm() != 0600 || pm.IsRunning() {
		t.Fatalf("unexpected apply state: %s %v", raw, info.Mode())
	}
	matches, _ := filepath.Glob(filepath.Join(pm.dataDir, ".candidate-*"))
	if len(matches) != 0 {
		t.Fatal("candidate secret files remain")
	}
}

func TestIdentityRejectsChangedStartTime(t *testing.T) {
	pm := newHelperManager(t)
	id := processIdentity{PID: 123, Created: 1, Executable: pm.singboxPath, Config: pm.configPath, Directory: pm.dataDir}
	pm.identity = id
	pm.inspect = func(int) (processIdentity, error) { changed := id; changed.Created++; return changed, nil }
	if pm.IsRunning() || pm.GetPID() != 0 {
		t.Fatal("reused PID was considered managed")
	}
	if err := pm.Stop(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryRequiresExactPathsAndStartTime(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	restored := NewProcessManager(pm.singboxPath, pm.configPath, pm.dataDir)
	if !restored.IsRunning() || restored.GetPID() != pm.GetPID() {
		t.Fatal("matching instance was not recovered")
	}
	// 不同配置不能通过同一个 PID 文件接管。
	other := NewProcessManager(pm.singboxPath, filepath.Join(pm.dataDir, "different.json"), pm.dataDir)
	if other.IsRunning() {
		t.Fatal("different config adopted same process")
	}
	if !pm.IsRunning() {
		t.Fatal("unrelated recovery killed the running instance")
	}
}

func TestApplyTransactionsSerialize(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for _, raw := range []string{`{"marker":"a"}`, `{"marker":"b"}`} {
		wg.Add(1)
		go func(raw string) { defer wg.Done(); errors <- pm.ApplyConfig([]byte(raw)) }(raw)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(pm.configPath)
	previous, _ := os.ReadFile(pm.configPath + ".previous")
	if !pm.IsRunning() || string(raw) == string(previous) || !json.Valid(previous) {
		t.Fatalf("inconsistent serialized result: %s / %s", raw, previous)
	}
}

func TestHealthFailureRestoresOriginalConfiguration(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	// 明确存在 Clash API 配置时，即使进程存活也必须等其本地 API 就绪。
	if err := pm.ApplyConfig([]byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:1"}}}`)); err == nil {
		t.Fatal("missing Clash health endpoint accepted")
	}
	if !pm.IsRunning() {
		t.Fatal("old process was not recovered")
	}
}

func TestConfigSecretsAreRedacted(t *testing.T) {
	redact := configRedactor([]byte(`{"outbounds":[{"password":"super-secret","uuid":"node-uuid","private_key":"key-value"}],"experimental":{"clash_api":{"secret":"api-secret"}}}`))
	got := redact("super-secret node-uuid key-value api-secret")
	if strings.Contains(got, "secret") || strings.Contains(got, "uuid") || strings.Contains(got, "key-value") {
		t.Fatalf("secret remains: %s", got)
	}
}

func TestKernelHealthFailureRestoresOldBinaryAndProcess(t *testing.T) {
	pm := newHelperManager(t)
	source, err := os.ReadFile(pm.singboxPath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(pm.dataDir, "bin", "sing-box")
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, source, 0755); err != nil {
		t.Fatal(err)
	}
	pm.SetPaths(target, pm.configPath)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(pm.dataDir, "candidate")
	script := "#!/bin/sh\ncase \"$1\" in\nversion) printf 'sing-box version 1.14.1\\n';;\ncheck) exit 0;;\nrun) exit 1;;\nesac\n"
	if err := os.WriteFile(candidate, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := pm.InstallKernel(candidate); err == nil {
		t.Fatal("expected candidate runtime failure")
	}
	restored, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, restored) || !pm.IsRunning() {
		t.Fatal("old kernel and process were not restored")
	}
	backup, err := os.ReadFile(target + ".previous")
	if err != nil || !bytes.Equal(backup, source) {
		t.Fatal("old kernel backup not retained")
	}
}

func TestKernelCandidateConfigCheckFailureDoesNotStopExistingInstance(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	pid := pm.GetPID()
	candidate := filepath.Join(pm.dataDir, "candidate")
	script := "#!/bin/sh\nif [ \"$1\" = version ]; then printf 'sing-box version 1.14.1\\n'; else exit 1; fi\n"
	if err := os.WriteFile(candidate, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if err := pm.InstallKernel(candidate); err == nil {
		t.Fatal("accepted incompatible candidate")
	}
	if !pm.IsRunning() || pm.GetPID() != pid {
		t.Fatal("validation failure stopped existing instance")
	}
}

func TestStaleMonitorDoesNotDeleteNewIdentityFile(t *testing.T) {
	pm := newHelperManager(t)
	old := processIdentity{PID: 1, Created: 100, Executable: pm.singboxPath, Config: pm.configPath, Directory: pm.dataDir}
	next := old
	next.PID = 2
	next.Created = 200
	pm.identity = old
	if err := pm.saveIdentity(next); err != nil {
		t.Fatal(err)
	}
	pm.clearIdentity(old)
	raw, err := os.ReadFile(pm.pidFile)
	if err != nil {
		t.Fatal("stale observer deleted new identity", err)
	}
	var saved processIdentity
	if err := json.Unmarshal(raw, &saved); err != nil || saved != next {
		t.Fatal("new identity changed")
	}
}

func TestManagedProcessPathsWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data with spaces")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pm := NewProcessManager(exe, filepath.Join(dir, "config with spaces.json"), dir)
	pm.healthWindow = 60 * time.Millisecond
	pm.healthTimeout = time.Second
	if err := os.WriteFile(pm.configPath, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pm.Stop(); err != nil {
			t.Error(err)
		}
	})
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	if !pm.IsRunning() {
		t.Fatal("cannot identify paths with spaces")
	}
}

func TestPathChangeCannotRedirectLiveApply(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Start(); err != nil {
		t.Fatal(err)
	}
	pid := pm.GetPID()
	other := filepath.Join(pm.dataDir, "other.json")
	pm.SetConfigPath(other)
	if err := pm.ApplyConfig([]byte(`{}`)); err == nil {
		t.Fatal("path change redirected live transaction")
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatal("unexpected config file created")
	}
	if pm.GetPID() != pid {
		t.Fatal("original instance was stopped")
	}
}

func TestReloadDoesNotStartStoppedInstance(t *testing.T) {
	pm := newHelperManager(t)
	if err := pm.Reload(); err == nil {
		t.Fatal("reload unexpectedly accepted stopped instance")
	}
	if pm.IsRunning() {
		t.Fatal("reload started stopped instance")
	}
}

func TestManagerExitKeepsKernelAndDetachedRedactedLogging(t *testing.T) {
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"emit_log":true,"password":"relay-private-secret"}`), 0600); err != nil {
		t.Fatal(err)
	}
	// 独立管理器子进程正常退出，测试进程随后恢复同一个仍运行的内核。
	if out, err := exec.Command(executable, "--manager-fixture", dir).CombinedOutput(); err != nil {
		t.Fatalf("manager fixture failed: %v %s", err, out)
	}
	pm := NewProcessManager(executable, filepath.Join(dir, "config.json"), dir)
	t.Cleanup(func() {
		if err := pm.Stop(); err != nil {
			t.Error(err)
		}
	})
	if !pm.IsRunning() {
		t.Fatal("kernel stopped when its manager exited")
	}
	first, err := os.Stat(filepath.Join(dir, "logs", "singbox.log"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(160 * time.Millisecond)
	later, err := os.Stat(filepath.Join(dir, "logs", "singbox.log"))
	if err != nil {
		t.Fatal(err)
	}
	if later.Size() <= first.Size() || !pm.IsRunning() {
		t.Fatal("logging stopped or killed detached kernel")
	}
	logs, err := os.ReadFile(filepath.Join(dir, "logs", "singbox.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logs), "relay-private-secret") || !strings.Contains(string(logs), "[REDACTED]") || later.Mode().Perm() != 0600 {
		t.Fatalf("unsafe detached logs: %s / %v", logs, later.Mode())
	}
	pgid, err := syscall.Getpgid(pm.GetPID())
	if err != nil || pgid != pm.GetPID() {
		t.Fatal("kernel was not isolated from manager process group")
	}
}
