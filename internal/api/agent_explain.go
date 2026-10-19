package api

import (
	"context"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/dnsquery"
	"github.com/xiaobei/singbox-manager/internal/runtimecontrol"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 静态解释只按配置文本推断；rule_set、IP、端口等需要运行时信息的条件标记为 maybe。
const (
	matchYes   = "match"
	matchNo    = "no"
	matchMaybe = "maybe"
)

var domainTypeKeys = map[string]bool{"domain": true, "domain_suffix": true, "domain_keyword": true, "domain_regex": true}
var addressKeys = map[string]bool{"ip_cidr": true, "ip_is_private": true, "rule_set": true, "geosite": true, "geoip": true}
var ignoredRuleKeys = map[string]bool{"type": true, "outbound": true, "action": true, "rule_set_ip_cidr_match_source": true}
var nonFinalActions = map[string]bool{"sniff": true, "resolve": true, "route-options": true}

func stringList(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := []string{}
		for _, item := range x {
			out = append(out, fmt.Sprint(item))
		}
		return out
	}
	return nil
}

func domainMatches(key string, values []string, host string) bool {
	for _, v := range values {
		v = strings.ToLower(v)
		switch key {
		case "domain":
			if host == v {
				return true
			}
		case "domain_suffix":
			trimmed := strings.TrimPrefix(v, ".")
			if (!strings.HasPrefix(v, ".") && host == trimmed) || strings.HasSuffix(host, "."+trimmed) {
				return true
			}
		case "domain_keyword":
			if strings.Contains(host, v) {
				return true
			}
		case "domain_regex":
			if re, err := regexp.Compile(v); err == nil && re.MatchString(host) {
				return true
			}
		}
	}
	return false
}

func sourceMatches(values []string, source netip.Addr) bool {
	for _, v := range values {
		if p, err := storage.SourcePrefix(v); err == nil && p.Contains(source) {
			return true
		}
	}
	return false
}

// evalRule 返回匹配结论与仍需运行时确认的条件。
func evalRule(rule map[string]any, host string, source *netip.Addr) (string, []string) {
	if rule["type"] == "logical" {
		return matchMaybe, []string{"logical 组合规则"}
	}
	unknown := []string{}
	domainKeys, hasAddress, domainHit := 0, false, false
	for key, value := range rule {
		switch {
		case domainTypeKeys[key]:
			domainKeys++
			if domainMatches(key, stringList(value), host) {
				domainHit = true
			}
		case addressKeys[key]:
			hasAddress = true
		}
	}
	result := matchYes
	if domainKeys+boolInt(hasAddress) > 0 && !domainHit {
		if !hasAddress {
			return matchNo, nil
		}
		result = matchMaybe
		unknown = append(unknown, "目的地址规则集/IP 条件")
	}
	for key, value := range rule {
		if domainTypeKeys[key] || addressKeys[key] || ignoredRuleKeys[key] {
			continue
		}
		if key == "invert" {
			if value == true {
				return matchMaybe, []string{"invert 反向条件"}
			}
			continue
		}
		if key == "source_ip_cidr" && source != nil {
			if !sourceMatches(stringList(value), *source) {
				return matchNo, nil
			}
			continue
		}
		result = matchMaybe
		unknown = append(unknown, key)
	}
	return result, unknown
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type routeStep struct {
	Index    int      `json:"index"`
	Result   string   `json:"result"`
	Action   string   `json:"action"`
	Outbound string   `json:"outbound,omitempty"`
	Needs    []string `json:"needs,omitempty"`
	Rule     any      `json:"rule"`
}

type routeVerdict struct {
	Available  bool        `json:"available"`
	Error      string      `json:"error,omitempty"`
	Outbound   string      `json:"outbound,omitempty"`
	Decided    string      `json:"decided_by,omitempty"`
	Matched    *routeStep  `json:"matched,omitempty"`
	MaybeFirst []routeStep `json:"may_match_first"`
}

func explainRoute(raw []byte, host string, source *netip.Addr) routeVerdict {
	v := routeVerdict{MaybeFirst: []routeStep{}}
	if len(raw) == 0 {
		v.Error = "没有可用配置"
		return v
	}
	route, err := readRouteSummary(raw)
	if err != nil {
		v.Error = err.Error()
		return v
	}
	v.Available = true
	for i, rule := range route.Rules {
		action, _ := rule["action"].(string)
		outbound, _ := rule["outbound"].(string)
		if action == "" {
			action = "route"
		}
		if nonFinalActions[action] || action == "hijack-dns" {
			continue
		}
		result, needs := evalRule(rule, host, source)
		if result == matchNo {
			continue
		}
		step := routeStep{Index: i, Result: result, Action: action, Outbound: outbound, Needs: needs, Rule: redactedJSON(rule)}
		if result == matchMaybe {
			if len(v.MaybeFirst) < 20 {
				v.MaybeFirst = append(v.MaybeFirst, step)
			}
			continue
		}
		v.Matched, v.Decided = &step, "rule"
		v.Outbound = outbound
		if action == "reject" {
			v.Outbound = "REJECT"
		}
		return v
	}
	v.Outbound, v.Decided = route.Final, "final"
	return v
}

var hostPattern = regexp.MustCompile(`^[a-z0-9_.-]{1,253}$`)

func (s *Server) runtimeConnectionsSnapshot(ctx context.Context) ([]runtimecontrol.Connection, string) {
	var conns []runtimecontrol.Connection
	err := s.withRuntime(ctx, "", func(client *runtimecontrol.Client, _ string) error {
		snapshot, err := client.Connections(ctx)
		conns = snapshot.Connections
		return err
	})
	if err != nil {
		return nil, "无法读取内核实时连接：" + err.Error()
	}
	return conns, ""
}

func connectionView(c runtimecontrol.Connection) gin.H {
	return gin.H{"source": c.Source, "host": c.Host, "destination": c.Destination, "destination_port": c.DestinationPort, "network": c.Network, "protocol": c.Protocol, "chains": c.Chains, "rule": c.Rule, "started_at": c.StartedAt}
}

func (s *Server) agentExplainDomain(c *gin.Context) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(c.Query("domain"))), ".")
	if !hostPattern.MatchString(host) || !strings.Contains(host, ".") {
		c.JSON(400, gin.H{"error": "需要完整域名，例如 domain=www.example.com", "code": "bad_request"})
		return
	}
	var source *netip.Addr
	if raw := c.Query("source"); raw != "" {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			c.JSON(400, gin.H{"error": "source 需要是 IP 地址", "code": "bad_request"})
			return
		}
		source = &addr
	}
	data := s.store.Snapshot()
	saved := routeVerdict{MaybeFirst: []routeStep{}}
	if draft, err := s.buildData(deepCopy(data), true); err != nil {
		saved.Error = "已保存配置无法生成：" + err.Error()
	} else {
		saved = explainRoute([]byte(draft), host, source)
	}
	appliedRaw, _, _ := s.readAppliedConfig()
	applied := explainRoute(appliedRaw, host, source)

	s.scanDNSQueries()
	dns := s.dnsQueries.Search(dnsquery.Filter{Search: host, Limit: 20, Sort: "recent"}, time.Now())
	records := []dnsquery.Record{}
	for _, r := range dns.Records {
		if r.Domain == host && (source == nil || r.Source == source.String()) {
			records = append(records, r)
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	conns, runtimeErr := s.runtimeConnectionsSnapshot(ctx)
	matched := []gin.H{}
	for _, conn := range conns {
		h := strings.ToLower(conn.Host)
		if (h == host || strings.HasSuffix(h, "."+host)) && (source == nil || conn.Source == source.String()) && len(matched) < 20 {
			matched = append(matched, connectionView(conn))
		}
	}
	c.JSON(200, gin.H{"data": gin.H{
		"domain": host, "source": c.Query("source"),
		"saved":          saved,
		"applied":        applied,
		"differs":        saved.Outbound != applied.Outbound,
		"runtime":        gin.H{"available": runtimeErr == "", "error": runtimeErr, "connections": matched},
		"dns":            gin.H{"recent": records, "status": s.dnsQueries.Status()},
		"classification": s.dnsDomainCheck.Check([]string{host}),
		"evidence": []string{
			"saved/applied 是对配置文本的静态推断：rule_set、IP、端口、协议等条件无法离线判定，列在 may_match_first 中",
			"runtime.connections 是内核当前活动连接的实际出站链路，是真实流量证据；没有连接不代表规则错误",
			"dns.recent 来自 LAN DNS 查询记录（需开启查询记录且只覆盖经过本实例的 DNS）",
		},
	}})
}

func (s *Server) agentExplainClient(c *gin.Context) {
	addr, err := netip.ParseAddr(c.Query("ip"))
	if err != nil {
		c.JSON(400, gin.H{"error": "需要 ip=设备地址", "code": "bad_request"})
		return
	}
	settings := s.store.GetSettings()
	devices := []gin.H{}
	for _, d := range devicePolicySummary(settings) {
		for _, a := range d["addresses"].([]string) {
			if p, e := storage.SourcePrefix(a); e == nil && p.Contains(addr) {
				devices = append(devices, d)
				break
			}
		}
	}
	appliedRaw, _, _ := s.readAppliedConfig()
	sourceRules := []gin.H{}
	if route, err := readRouteSummary(appliedRaw); err == nil {
		for i, rule := range route.Rules {
			if values := stringList(rule["source_ip_cidr"]); len(values) > 0 && sourceMatches(values, addr) {
				sourceRules = append(sourceRules, gin.H{"index": i, "rule": redactedJSON(rule)})
			}
		}
	}
	s.scanDNSQueries()
	dns := s.dnsQueries.Search(dnsquery.Filter{Source: addr.String(), Limit: 30, Sort: "recent"}, time.Now())
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	conns, runtimeErr := s.runtimeConnectionsSnapshot(ctx)
	matched := []gin.H{}
	for _, conn := range conns {
		if conn.Source == addr.String() && len(matched) < 30 {
			matched = append(matched, connectionView(conn))
		}
	}
	c.JSON(200, gin.H{"data": gin.H{
		"ip": addr.String(), "role": settings.DeploymentRole,
		"device_policy":        devices,
		"applied_source_rules": sourceRules,
		"dns":                  gin.H{"recent": dns.Records, "top_domains": dns.Domains},
		"runtime":              gin.H{"available": runtimeErr == "", "error": runtimeErr, "connections": matched},
		"evidence": []string{
			"device_policy 是已保存设备策略的解释，不能证明流量经过本实例",
			"runtime.connections 与 dns.recent 是真实流量与查询证据；为空可能是设备未使用本实例或当前无流量",
		},
	}})
}
