package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func readLinuxBootID() (string, error) {
	data, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		return "", e
	}
	return strings.TrimSpace(string(data)), nil
}
func (m *Manager) currentBootID() (string, error) {
	reader := m.ReadBootID
	if reader == nil {
		reader = readLinuxBootID
	}
	id, e := reader()
	if e != nil {
		return "", fmt.Errorf("无法读取 Linux boot ID: %w", e)
	}
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return "", fmt.Errorf("Linux boot ID 无效")
	}
	return id, nil
}

// bootBoundary 在读取旧快照对应的 sysctl 前执行；不把缺失 boot ID 当作同一次启动。
func (m *Manager) bootBoundary(s state) (string, bool, *Finding) {
	if s.Active == nil && s.Pending == nil {
		return "", false, nil
	}
	current, e := m.currentBootID()
	if e != nil {
		return "", false, &Finding{"error", "boot_unavailable", "无法读取本次系统启动身份，不能安全使用旧运行态快照"}
	}
	if s.BootID == "" {
		return current, false, &Finding{"error", "boot_unknown", "旧状态缺少系统启动身份；需先显式恢复并归档，再重新检查和应用"}
	}
	if s.BootID != current {
		return current, true, &Finding{"error", "rebooted", "检测到主机重启；旧运行态快照不能写入本次启动，请先恢复归档再重新检查和应用"}
	}
	return current, false, nil
}

// recoverPreviousBoot 只归档并释放旧启动的记录，不向新启动回写任何 sysctl、nft 或服务状态。
func (m *Manager) recoverPreviousBoot(ctx context.Context, s state, current string, result Status) (Status, error) {
	conflicts, e := m.previousBootConflicts(ctx, s)
	if e != nil {
		return result, e
	}
	if len(conflicts) > 0 {
		result.Findings = append(result.Findings, conflicts...)
		return result, fmt.Errorf("检测到本次启动的接管资源或冲突服务，拒绝清除旧归属记录")
	}
	data, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return result, e
	}
	archive := filepath.Join(m.StateDir, "state-previous-boot-"+digest(string(data))+".json")
	if existing, e := os.ReadFile(archive); e == nil {
		if string(existing) != string(data) {
			return result, fmt.Errorf("旧启动归档文件已被修改，拒绝覆盖")
		}
	} else if !os.IsNotExist(e) {
		return result, e
	} else if e = atomicWrite(archive, data); e != nil {
		return result, e
	}
	// 归档先持久化；清除中断后可重复执行，不丢失原始记录。
	if e = m.save(state{BootID: current}); e != nil {
		return result, e
	}
	return Status{Findings: []Finding{{"info", "previous_boot_archived", "旧启动期状态已归档；没有回写旧系统参数，请重新检查并显式应用网关"}}}, nil
}

func (m *Manager) previousBootConflicts(ctx context.Context, s state) ([]Finding, error) {
	findings := []Finding{}
	add := func(code, message string) { findings = append(findings, Finding{"error", code, message}) }
	nft, e := m.nft(ctx)
	if e != nil {
		return findings, e
	}
	if nft != "" {
		add("reboot_owned_table", "当前启动仍存在 sbm_gateway 表，必须由管理员核对，不能按旧启动快照删除")
	}
	stateValue, e := m.run(ctx, "systemctl", "is-active", "sbm-gateway-dhcp.service")
	stateValue = strings.TrimSpace(stateValue)
	switch stateValue {
	case "inactive", "failed", "unknown":
	case "active", "activating", "deactivating", "reloading":
		add("reboot_dhcp_active", "专用 DHCP 服务仍在运行或切换状态，请先安全停止该服务")
	default:
		if e != nil {
			return findings, fmt.Errorf("无法确认 DHCP 服务已停止")
		}
		add("reboot_dhcp_unknown", "DHCP 服务状态未知，拒绝清除归属记录")
	}
	rules, e := m.run(ctx, "nft", "list", "ruleset")
	if e != nil {
		return findings, e
	}
	if hasSingboxTable(rules) || strings.Contains(strings.ToLower(rules), "tproxy") {
		add("reboot_takeover", "当前启动存在 sing-box/TProxy 接管规则，请先核对并停止冲突实例")
	}
	configs := []Config{}
	if s.Active != nil {
		configs = append(configs, s.Active.Plan.Config)
	}
	if s.Pending != nil {
		configs = append(configs, s.Pending.Target.Config)
	}
	listeners, e := m.run(ctx, "ss", "-H", "-lntup")
	if e != nil {
		return findings, e
	}
	for _, line := range strings.Split(listeners, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		endpoint := fields[4]
		idx := strings.LastIndex(endpoint, ":")
		if idx < 0 {
			continue
		}
		port := endpoint[idx+1:]
		host := strings.Trim(endpoint[:idx], "[]")
		conflict := port == "67" || port == "5354" || port == "9888" || port == fmt.Sprint(TProxyPort)
		for _, c := range configs {
			c = Normalize(c)
			if port == fmt.Sprint(c.DNSPort) && (host == "*" || host == "0.0.0.0" || host == "::" || host == c.LANAddress) {
				conflict = true
			}
		}
		if conflict {
			add("reboot_listener", "当前启动仍有 DHCP/旧接管/LAN DNS 监听，请先核对并停止冲突实例")
			break
		}
	}
	for _, family := range []string{"-4", "-6"} {
		rules, e := m.run(ctx, "ip", "-N", family, "rule", "show")
		if e != nil {
			return findings, e
		}
		for _, line := range strings.Split(rules, "\n") {
			if reservedDNSRule(line) || hasPair(line, "lookup", "100") || hasPair(line, "fwmark", "0x1") || hasPair(line, "lookup", "2022") || strings.HasPrefix(strings.TrimSpace(line), "9000:") {
				add("reboot_policy_route", "当前启动仍有旧透明代理/TUN 策略路由，请先核对其归属")
				break
			}
		}
	}
	dns, e := m.captureDNSRouting(ctx)
	if e != nil {
		return findings, e
	}
	if dns != (dnsRouting{}) {
		add("reboot_dns_route", "本次启动存在 DNS 旁路路由资源，不能按旧启动归属删除")
	}
	for _, legacy := range []struct{ tool, path string }{{"iptables-save", "/proc/net/ip_tables_names"}, {"ip6tables-save", "/proc/net/ip6_tables_names"}} {
		rules, e := m.run(ctx, legacy.tool)
		if e != nil {
			if names, readErr := os.ReadFile(legacy.path); readErr == nil && strings.TrimSpace(string(names)) != "" {
				return findings, fmt.Errorf("无法读取已加载的 legacy iptables 表")
			}
			continue
		}
		if strings.Contains(strings.ToUpper(rules), "-J TPROXY") {
			add("reboot_legacy_tproxy", "当前启动仍有 legacy iptables TProxy 接管")
		}
	}
	return findings, nil
}
