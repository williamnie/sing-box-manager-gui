package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRebootArchivesStateWithoutReplayingOldSystemParameters(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	prior, e := m.load()
	if e != nil || prior.BootID != "test-boot-one" {
		t.Fatal("boot identity not persisted", e)
	}
	// 模拟新启动中 nft 丢失、参数来自另一套当前启动配置。
	r.nft = ""
	r.sysctls["net.ipv4.ip_forward"] = "0"
	r.sysctls["net.ipv4.conf.all.rp_filter"] = "2"
	m.ReadBootID = func() (string, error) { return "test-boot-two", nil }
	mutations := r.mutations
	r.fail = func(name string, _ []string, _ string) bool { return name == "sysctl" }
	status, e := m.Status(ctx)
	if e != nil || status.Applied || status.Drift || !status.Rebooted || !status.RecoveryRequired || !findingCode(status.Findings, "rebooted") {
		t.Fatalf("reboot status: %+v %v", status, e)
	}
	checked, e := m.Check(ctx, "gateway", c)
	if e != nil || checked.Ready || !findingCode(checked.Findings, "rebooted") {
		t.Fatalf("reboot check: %+v %v", checked, e)
	}
	if _, e = m.Apply(ctx, "gateway", c); e == nil {
		t.Fatal("reboot state automatically reapplied")
	}
	if _, e = m.RestartManager(ctx); e == nil {
		t.Fatal("restart with unrecovered boot allowed")
	}
	restored, e := m.Rollback(ctx)
	if e != nil || restored.Applied || restored.RecoveryRequired || !findingCode(restored.Findings, "previous_boot_archived") {
		t.Fatalf("reboot recovery: %+v %v", restored, e)
	}
	if r.mutations != mutations || r.sysctls["net.ipv4.conf.all.rp_filter"] != "2" {
		t.Fatal("old boot parameters replayed")
	}
	current, e := m.load()
	if e != nil || current.Active != nil || current.Pending != nil || current.BootID != "test-boot-two" {
		t.Fatal("old state not cleared", e)
	}
	paths, e := filepath.Glob(filepath.Join(m.StateDir, "state-previous-boot-*.json"))
	if e != nil || len(paths) != 1 {
		t.Fatal("missing durable previous boot archive", paths, e)
	}
	raw, e := os.ReadFile(paths[0])
	if e != nil {
		t.Fatal(e)
	}
	var archived state
	if e = json.Unmarshal(raw, &archived); e != nil || archived.Active == nil || archived.BootID != prior.BootID {
		t.Fatal("archive lost original state", e)
	}
	if _, e = m.Rollback(ctx); e != nil || r.mutations != mutations {
		t.Fatal("repeated archive recovery changed system", e)
	}
	// 重新显式启用获取新启动的 baseline，后续恢复也不能用旧值。
	r.fail = nil
	if _, e = m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	if r.sysctls["net.ipv4.conf.all.rp_filter"] != "2" {
		t.Fatal("reapplication reused stale baseline")
	}
}

func TestSameBootDriftCannotUseRebootRecovery(t *testing.T) {
	m, r, c := fixture(t)
	ctx := context.Background()
	if _, e := m.Apply(ctx, "gateway", c); e != nil {
		t.Fatal(e)
	}
	r.nft = ""
	before := r.mutations
	status, e := m.Status(ctx)
	if e != nil || !status.Applied || !status.Drift || status.Rebooted {
		t.Fatalf("same boot drift: %+v %v", status, e)
	}
	if _, e = m.Rollback(ctx); e == nil {
		t.Fatal("same boot drift silently released")
	}
	if r.mutations != before {
		t.Fatal("same boot drift overwritten")
	}
	saved, e := m.load()
	if e != nil || saved.Active == nil {
		t.Fatal("same boot ownership lost")
	}
	paths, _ := filepath.Glob(filepath.Join(m.StateDir, "state-previous-boot-*.json"))
	if len(paths) != 0 {
		t.Fatal("same boot drift archived as reboot")
	}
}

func TestRebootRecoveryRefusesRemainingResources(t *testing.T) {
	for _, conflict := range []string{"owned_table", "dhcp_service", "dns_listener", "tproxy", "other_tun"} {
		t.Run(conflict, func(t *testing.T) {
			m, r, c := fixture(t)
			ctx := context.Background()
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			m.ReadBootID = func() (string, error) { return "new-boot", nil }
			r.nft = ""
			switch conflict {
			case "owned_table":
				r.nft = "table inet sbm_gateway {}"
			case "dhcp_service":
				r.dhcpActive = true
			case "dns_listener":
				r.listeners = "udp UNCONN 0 0 192.0.2.142:53 0.0.0.0:*"
			case "tproxy":
				r.rules = "tproxy to :9888"
			case "other_tun":
				r.rules = "table ip sing-box {}"
			}
			before := r.mutations
			v, e := m.Rollback(ctx)
			if e == nil || !v.Rebooted || !v.RecoveryRequired {
				t.Fatalf("remaining resource ignored: %+v %v", v, e)
			}
			if r.mutations != before {
				t.Fatal("current boot resources touched")
			}
			s, e := m.load()
			if e != nil || s.Active == nil {
				t.Fatal("ownership cleared despite resources")
			}
		})
	}
}

func TestRebootArchivesPendingTransactionWithoutResumingIt(t *testing.T) {
	m, r, c := fixture(t)
	p, e := Preview("gateway", c)
	if e != nil {
		t.Fatal(e)
	}
	s := state{Pending: &transaction{Target: p, Before: snapshot{Sysctls: map[string]string{"net.ipv4.ip_forward": "0"}}}}
	if e = m.save(s); e != nil {
		t.Fatal(e)
	}
	m.ReadBootID = func() (string, error) { return "new-boot", nil }
	r.sysctls["net.ipv4.ip_forward"] = "1"
	if _, e = m.Rollback(context.Background()); e != nil {
		t.Fatal(e)
	}
	if r.mutations != 0 || r.sysctls["net.ipv4.ip_forward"] != "1" {
		t.Fatal("old pending transaction resumed in new boot")
	}
}

func TestUnknownAndUnreadableBootNeverReuseSnapshot(t *testing.T) {
	for _, mode := range []string{"missing_identity", "unreadable_identity"} {
		t.Run(mode, func(t *testing.T) {
			m, r, c := fixture(t)
			ctx := context.Background()
			if _, e := m.Apply(ctx, "gateway", c); e != nil {
				t.Fatal(e)
			}
			r.nft = ""
			before := r.mutations
			if mode == "missing_identity" {
				s, e := m.load()
				if e != nil {
					t.Fatal(e)
				}
				s.BootID = ""
				b, e := json.Marshal(s)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(m.StateDir, "state.json"), b, 0600); e != nil {
					t.Fatal(e)
				}
			} else {
				m.ReadBootID = func() (string, error) { return "", errors.New("unavailable") }
			}
			v, e := m.Status(ctx)
			if e != nil || v.Applied || v.Rebooted || !v.RecoveryRequired {
				t.Fatalf("unknown boot state: %+v %v", v, e)
			}
			if _, e = m.Apply(ctx, "gateway", c); e == nil {
				t.Fatal("unknown boot application allowed")
			}
			_, e = m.Rollback(ctx)
			if mode == "missing_identity" && e != nil {
				t.Fatal("safe legacy archive failed", e)
			}
			if mode == "unreadable_identity" && e == nil {
				t.Fatal("unverified current boot archived")
			}
			if r.mutations != before {
				t.Fatal("unknown boot modified system")
			}
		})
	}
}

func findingCode(findings []Finding, code string) bool {
	for _, v := range findings {
		if v.Code == code {
			return true
		}
	}
	return false
}

func TestApplyIdleStateAfterRebootUsesCurrentBoot(t *testing.T) {
	for _, mode := range []string{"full", "dns"} {
		t.Run(mode, func(t *testing.T) {
			m, _, c := fixture(t)
			if mode == "dns" {
				m, _, c = dnsFixture(t)
			}
			// 从未启用或已恢复的空状态可能保留上一次启动的身份。
			if err := m.save(state{BootID: "previous-boot"}); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := m.Apply(ctx, "gateway", c); err != nil {
				t.Fatal(err)
			}
			status, err := m.Status(ctx)
			if err != nil || !status.Applied || status.Rebooted || status.RecoveryRequired {
				t.Fatalf("fresh application retained stale boot identity: %+v %v", status, err)
			}
			if _, err = m.Rollback(ctx); err != nil {
				t.Fatal("fresh application cannot be restored", err)
			}
		})
	}
}
