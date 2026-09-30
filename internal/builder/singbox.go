package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/gateway"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// SingBoxConfig sing-box 配置结构
type SingBoxConfig struct {
	Log          *LogConfig          `json:"log,omitempty"`
	DNS          *DNSConfig          `json:"dns,omitempty"`
	NTP          *NTPConfig          `json:"ntp,omitempty"`
	Inbounds     []Inbound           `json:"inbounds,omitempty"`
	Outbounds    []Outbound          `json:"outbounds"`
	Route        *RouteConfig        `json:"route,omitempty"`
	Experimental *ExperimentalConfig `json:"experimental,omitempty"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level     string `json:"level,omitempty"`
	Timestamp bool   `json:"timestamp,omitempty"`
	Output    string `json:"output,omitempty"`
}

// DNSConfig DNS 配置
type DNSConfig struct {
	Raw              map[string]any `json:"-"`
	Strategy         string         `json:"strategy,omitempty"`
	Servers          []DNSServer    `json:"servers,omitempty"`
	Rules            []DNSRule      `json:"rules,omitempty"`
	Final            string         `json:"final,omitempty"`
	IndependentCache bool           `json:"independent_cache,omitempty"`
}

// DNSServer DNS 服务器 (新格式，支持 FakeIP 和 hosts)
type DNSServer struct {
	ServerPort     int            `json:"server_port,omitempty"`
	Path           string         `json:"path,omitempty"`
	DomainResolver string         `json:"domain_resolver,omitempty"`
	Tag            string         `json:"tag"`
	Type           string         `json:"type"`                  // udp, tcp, https, tls, quic, h3, fakeip, rcode, hosts
	Server         string         `json:"server,omitempty"`      // 服务器地址
	Detour         string         `json:"detour,omitempty"`      // 出站代理
	Inet4Range     string         `json:"inet4_range,omitempty"` // FakeIP IPv4 地址池
	Inet6Range     string         `json:"inet6_range,omitempty"` // FakeIP IPv6 地址池
	Predefined     map[string]any `json:"predefined,omitempty"`  // hosts 类型专用：预定义域名映射
}

// DNSRule DNS 规则
type DNSRule struct {
	SourceCIDRs   []string `json:"source_ip_cidr,omitempty"`
	DomainSuffix  []string `json:"domain_suffix,omitempty"`
	DomainKeyword []string `json:"domain_keyword,omitempty"`
	Outbound      string   `json:"outbound,omitempty"` // 匹配出站的 DNS 查询，如 "any" 表示代理服务器地址解析
	RuleSet       []string `json:"rule_set,omitempty"`
	QueryType     []string `json:"query_type,omitempty"`
	Domain        []string `json:"domain,omitempty"` // 完整域名匹配
	Server        string   `json:"server,omitempty"`
	Action        string   `json:"action,omitempty"` // route, reject 等
}

// NTPConfig NTP 配置
type NTPConfig struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server,omitempty"`
}

// Inbound 入站配置
type Inbound struct {
	InterfaceName            string   `json:"interface_name,omitempty"`
	AutoRedirect             bool     `json:"auto_redirect,omitempty"`
	IncludeInterface         []string `json:"include_interface,omitempty"`
	RouteExcludeAddress      []string `json:"route_exclude_address,omitempty"`
	DNSMode                  string   `json:"dns_mode,omitempty"`
	Type                     string   `json:"type"`
	Tag                      string   `json:"tag"`
	Listen                   string   `json:"listen,omitempty"`
	ListenPort               int      `json:"listen_port,omitempty"`
	Address                  []string `json:"address,omitempty"`
	AutoRoute                bool     `json:"auto_route,omitempty"`
	StrictRoute              bool     `json:"strict_route,omitempty"`
	Stack                    string   `json:"stack,omitempty"`
	Sniff                    bool     `json:"sniff,omitempty"`
	SniffOverrideDestination bool     `json:"sniff_override_destination,omitempty"`
}

// Outbound 出站配置
type Outbound map[string]interface{}

// DomainResolver 域名解析器配置
type DomainResolver struct {
	Server     string `json:"server"`
	RewriteTTL int    `json:"rewrite_ttl,omitempty"`
}

// RouteConfig 路由配置
type RouteConfig struct {
	DefaultInterface      string          `json:"default_interface,omitempty"`
	Rules                 []RouteRule     `json:"rules,omitempty"`
	RuleSet               []RuleSet       `json:"rule_set,omitempty"`
	Final                 string          `json:"final,omitempty"`
	AutoDetectInterface   bool            `json:"auto_detect_interface,omitempty"`
	DefaultDomainResolver *DomainResolver `json:"default_domain_resolver,omitempty"`
}

// RouteRule 路由规则
type RouteRule map[string]interface{}

// RuleSet 规则集
type RuleSet struct {
	Raw            map[string]any `json:"-"`
	Tag            string         `json:"tag"`
	Type           string         `json:"type"`
	Format         string         `json:"format"`
	URL            string         `json:"url,omitempty"`
	DownloadDetour string         `json:"download_detour,omitempty"`
}

// ExperimentalConfig 实验性配置
type ExperimentalConfig struct {
	ClashAPI  *ClashAPIConfig  `json:"clash_api,omitempty"`
	CacheFile *CacheFileConfig `json:"cache_file,omitempty"`
}

// ClashAPIConfig Clash API 配置
type ClashAPIConfig struct {
	ExternalController    string `json:"external_controller,omitempty"`
	ExternalUI            string `json:"external_ui,omitempty"`
	ExternalUIDownloadURL string `json:"external_ui_download_url,omitempty"`
	Secret                string `json:"secret,omitempty"`
	DefaultMode           string `json:"default_mode,omitempty"`
}

// CacheFileConfig 缓存文件配置
type CacheFileConfig struct {
	Enabled     bool   `json:"enabled"`
	Path        string `json:"path,omitempty"`
	StoreFakeIP bool   `json:"store_fakeip,omitempty"` // 持久化 FakeIP 映射
}

// ConfigBuilder 配置生成器
type ConfigBuilder struct {
	settings   *storage.Settings
	nodes      []storage.Node
	filters    []storage.Filter
	rules      []storage.Rule
	ruleGroups []storage.RuleGroup
	profile    CompatProfile
	platform   string
}

// NewConfigBuilder 创建配置生成器
func NewConfigBuilder(settings *storage.Settings, nodes []storage.Node, filters []storage.Filter, rules []storage.Rule, ruleGroups []storage.RuleGroup) *ConfigBuilder {
	return &ConfigBuilder{
		settings:   settings,
		nodes:      nodes,
		filters:    filters,
		rules:      rules,
		ruleGroups: ruleGroups,
		profile:    DefaultCompatProfile(),
		platform:   runtime.GOOS,
	}
}

// WithSingBoxVersion 根据 sing-box version 输出应用兼容配置。
func (b *ConfigBuilder) WithSingBoxVersion(versionOutput string) *ConfigBuilder {
	b.profile = CompatProfileFromVersion(versionOutput)
	return b
}

// buildRuleSetURL 构建规则集 URL（支持 GitHub 代理）
func (b *ConfigBuilder) buildRuleSetURL(originalURL string) string {
	if b.settings.GithubProxy != "" {
		return b.settings.GithubProxy + originalURL
	}
	return originalURL
}

// Build 构建 sing-box 配置
func (b *ConfigBuilder) Build() (*SingBoxConfig, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}
	config := &SingBoxConfig{
		Log:       b.buildLog(),
		DNS:       b.buildDNS(),
		NTP:       b.buildNTP(),
		Inbounds:  b.buildInbounds(),
		Outbounds: b.buildOutbounds(),
		Route:     b.buildRoute(),
	}

	// 添加 Clash API 支持
	if b.settings.ClashAPIPort > 0 {
		config.Experimental = b.buildExperimental()
	}

	if err := b.applyImported(config); err != nil {
		return nil, err
	}
	if err := b.validateOutbounds(config); err != nil {
		return nil, err
	}
	if b.dnsBypass() {
		if err := b.configureDNSBypass(config); err != nil {
			return nil, err
		}
	}
	return config, nil
}

// BuildJSON 构建 JSON 字符串
func (b *ConfigBuilder) BuildJSON() (string, error) {
	config, err := b.Build()
	if err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化配置失败: %w", err)
	}

	return string(data), nil
}

// buildLog 构建日志配置
func (b *ConfigBuilder) buildLog() *LogConfig {
	level := b.settings.LogLevel
	if level == "" {
		level = "info"
	}
	return &LogConfig{
		Level:     level,
		Timestamp: true,
	}
}

// ParseSystemHosts 解析系统 /etc/hosts 文件
func ParseSystemHosts() map[string][]string {
	hosts := make(map[string][]string)

	data, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return hosts
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 去除行内注释
		if idx := strings.Index(line, "#"); idx != -1 {
			line = line[:idx]
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		ip := fields[0]
		// 跳过 localhost 相关条目
		for _, domain := range fields[1:] {
			if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
				continue
			}
			hosts[domain] = append(hosts[domain], ip)
		}
	}

	return hosts
}

// buildDNS 构建 DNS 配置
func (b *ConfigBuilder) buildDNS() *DNSConfig {
	proxy, _ := parseDNSServer(b.settings.ProxyDNS, "dns_proxy", "Proxy")
	direct, _ := parseDNSServer(b.settings.DirectDNS, "dns_direct", "")
	servers := []DNSServer{proxy, direct, {Tag: "dns_bootstrap", Type: "udp", Server: "223.5.5.5"}}
	if b.settings.DeploymentRole == "gateway" {
		for _, g := range b.settings.DeviceGroups {
			if g.Policy == "strict" {
				out := g.Outbound
				if out == "" {
					out = "Proxy"
				}
				server, _ := parseDNSServer(b.settings.ProxyDNS, "dns_strict_"+g.ID, out)
				servers = append(servers, server)
			}
		}
	}
	rules := b.policyDNSRules()
	// 家庭网关使用真实地址，避免来源分组和远端 DNS 缓存混用 FakeIP。
	if b.settings.DeploymentRole != "gateway" {
		servers = append(servers, DNSServer{Tag: "dns_fakeip", Type: "fakeip", Inet4Range: "198.18.0.0/15", Inet6Range: "fc00::/18"})
	}
	for _, rg := range b.ruleGroups {
		if !rg.Enabled || len(rg.SiteRules) == 0 {
			continue
		}
		tags := []string{}
		for _, v := range rg.SiteRules {
			tags = append(tags, "geosite-"+v)
		}
		if rg.Outbound == "REJECT" {
			rules = append(rules, DNSRule{RuleSet: tags, Action: "reject"})
		} else if rg.Outbound == "DIRECT" {
			rules = append(rules, DNSRule{RuleSet: tags, Server: "dns_direct", Action: "route"})
		}
	}
	if b.settings.DeploymentRole != "gateway" {
		rules = append(rules, DNSRule{QueryType: []string{"A", "AAAA"}, Server: "dns_fakeip", Action: "route"})
	}

	// 1. 读取系统 hosts
	systemHosts := b.systemHosts()

	// 2. 收集用户自定义 hosts（用户优先，会覆盖系统 hosts）
	predefined := make(map[string]any)
	var domains []string

	// 先添加系统 hosts
	for domain, ips := range systemHosts {
		if len(ips) == 1 {
			predefined[domain] = ips[0]
		} else {
			predefined[domain] = ips
		}
		domains = append(domains, domain)
	}

	// 再添加用户 hosts（覆盖同名系统 hosts）
	for _, host := range b.settings.Hosts {
		if host.Enabled && host.Domain != "" && len(host.IPs) > 0 {
			if len(host.IPs) == 1 {
				predefined[host.Domain] = host.IPs[0]
			} else {
				predefined[host.Domain] = host.IPs
			}
			// 如果是新域名，加入列表
			if _, exists := systemHosts[host.Domain]; !exists {
				domains = append(domains, host.Domain)
			}
		}
	}

	// 3. 如果有映射，添加 hosts 服务器和规则
	if len(predefined) > 0 {
		// 在服务器列表开头插入 hosts 服务器
		hostsServer := DNSServer{
			Tag:        "dns_hosts",
			Type:       "hosts",
			Predefined: predefined,
		}
		servers = append([]DNSServer{hostsServer}, servers...)

		// 在规则列表开头插入 hosts 规则（优先匹配）
		hostsRule := DNSRule{
			Domain: domains,
			Server: "dns_hosts",
			Action: "route",
		}
		rules = append([]DNSRule{hostsRule}, rules...)
	}

	sort.Strings(domains)
	return &DNSConfig{
		Strategy:         "prefer_ipv4",
		Servers:          servers,
		Rules:            rules,
		Final:            "dns_proxy",
		IndependentCache: true,
	}
}

// buildNTP 构建 NTP 配置
func (b *ConfigBuilder) buildNTP() *NTPConfig {
	return &NTPConfig{
		Enabled: true,
		Server:  "time.apple.com",
	}
}

// buildInbounds 构建入站配置
func (b *ConfigBuilder) buildInbounds() []Inbound {
	// mixed 端口仍按局域网访问设置监听。
	listenAddr := "127.0.0.1"
	if b.settings.AllowLAN && b.settings.DeploymentRole != "gateway" {
		listenAddr = "0.0.0.0"
	}

	inbounds := []Inbound{
		{
			Type:       "mixed",
			Tag:        "mixed-in",
			Listen:     listenAddr,
			ListenPort: b.settings.MixedPort,
		},
	}

	if b.profile.LegacyInboundFields {
		inbounds[0].Sniff = true
		inbounds[0].SniffOverrideDestination = true
	}

	if !b.dnsBypass() && (b.settings.TunEnabled || b.settings.DeploymentRole == "gateway") {
		tunInbound := Inbound{
			Type:        "tun",
			Tag:         "tun-in",
			Address:     []string{"172.19.0.1/30", "fdfe:dcba:9876::1/126"},
			AutoRoute:   true,
			StrictRoute: true,
			Stack:       "system",
		}
		if b.profile.LegacyInboundFields {
			tunInbound.Sniff = true
			tunInbound.SniffOverrideDestination = true
		}
		if b.settings.DeploymentRole == "gateway" {
			tunInbound.InterfaceName = "sbm-tun"
			tunInbound.AutoRedirect = true
			tunInbound.DNSMode = "hijack"
			tunInbound.IncludeInterface = []string{b.settings.Gateway.LANInterface}
			tunInbound.RouteExcludeAddress = b.settings.Gateway.ExcludeCIDRs
			if b.settings.Gateway.IPv6Mode == "disabled" {
				tunInbound.Address = tunInbound.Address[:1]
			}
		}
		inbounds = append(inbounds, tunInbound)
	}

	if b.settings.DeploymentRole == "gateway" {
		port := b.settings.Gateway.DNSPort
		if port == 0 {
			port = 53
		}
		inbounds = append(inbounds, Inbound{Type: "direct", Tag: "lan-dns", Listen: b.settings.Gateway.LANAddress, ListenPort: port})
	}
	if b.dnsBypass() {
		inbounds = append(inbounds, Inbound{Type: "tproxy", Tag: "dns-tproxy", Listen: "0.0.0.0", ListenPort: gateway.TProxyPort})
	}
	return inbounds
}

// buildOutbounds 构建出站配置
func (b *ConfigBuilder) buildOutbounds() []Outbound {
	outbounds := []Outbound{
		{"type": "direct", "tag": "DIRECT"},
		{"type": "block", "tag": "REJECT"},
		// 移除 dns-out，改用路由 action: hijack-dns
	}

	// 收集所有节点标签和按国家分组
	var allNodeTags []string
	nodeTagSet := make(map[string]bool)
	countryNodes := make(map[string][]string) // 国家代码 -> 节点标签列表

	// 添加所有节点
	for _, node := range b.nodes {
		outbound := b.nodeToOutbound(node)
		outbounds = append(outbounds, outbound)
		tag := node.Tag
		if !nodeTagSet[tag] {
			allNodeTags = append(allNodeTags, tag)
			nodeTagSet[tag] = true
		}

		// 按国家分组
		if node.Country != "" {
			countryNodes[node.Country] = append(countryNodes[node.Country], tag)
		} else {
			// 未识别国家的节点归入 "其他" 分组
			countryNodes["OTHER"] = append(countryNodes["OTHER"], tag)
		}
	}

	// 收集过滤器分组
	var filterGroupTags []string
	filterNodeMap := make(map[string][]string)

	for _, filter := range b.filters {
		if !filter.Enabled {
			continue
		}

		// 根据过滤器筛选节点
		var filteredTags []string
		for _, node := range b.nodes {
			if b.matchFilter(node, filter) {
				filteredTags = append(filteredTags, node.Tag)
			}
		}

		if len(filteredTags) == 0 {
			continue
		}

		groupTag := filter.Name
		filterGroupTags = append(filterGroupTags, groupTag)
		filterNodeMap[groupTag] = filteredTags

		// 管理界面的 select 模式对应 sing-box 的 selector 出站类型。
		groupType := filter.Mode
		if groupType == "select" {
			groupType = "selector"
		}
		// 创建分组
		group := Outbound{
			"tag":       groupTag,
			"type":      groupType,
			"outbounds": filteredTags,
		}

		if filter.Mode == "urltest" {
			if filter.URLTestConfig != nil {
				group["url"] = filter.URLTestConfig.URL
				group["interval"] = filter.URLTestConfig.Interval
				group["tolerance"] = filter.URLTestConfig.Tolerance
			} else {
				group["url"] = "https://www.gstatic.com/generate_204"
				group["interval"] = "5m"
				group["tolerance"] = 50
			}
		}

		outbounds = append(outbounds, group)
	}

	// 创建按国家分组的出站选择器
	var countryGroupTags []string
	// 按国家代码排序，确保顺序一致
	var countryCodes []string
	for code := range countryNodes {
		countryCodes = append(countryCodes, code)
	}
	sort.Strings(countryCodes)

	for _, code := range countryCodes {
		nodes := countryNodes[code]
		if len(nodes) == 0 {
			continue
		}

		// 创建国家分组标签，格式: "🇭🇰 香港" 或 "HK"
		emoji := storage.GetCountryEmoji(code)
		name := storage.GetCountryName(code)
		groupTag := fmt.Sprintf("%s %s", emoji, name)
		countryGroupTags = append(countryGroupTags, groupTag)

		// 创建自动选择分组
		outbounds = append(outbounds, Outbound{
			"tag":       groupTag,
			"type":      "urltest",
			"outbounds": nodes,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "5m",
			"tolerance": 50,
		})
	}

	// 创建自动选择组（所有节点）
	if len(allNodeTags) > 0 {
		outbounds = append(outbounds, Outbound{
			"tag":       "Auto",
			"type":      "urltest",
			"outbounds": allNodeTags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "5m",
			"tolerance": 50,
		})
	}

	// 创建主选择器（精简版：只包含分组，不包含单节点）
	proxyOutbounds := []string{"Auto"}
	if len(allNodeTags) == 0 {
		outbounds = append(outbounds, Outbound{"type": "selector", "tag": "Auto", "outbounds": []string{"DIRECT"}, "default": "DIRECT"})
	}
	proxyOutbounds = append(proxyOutbounds, countryGroupTags...) // 添加国家分组
	proxyOutbounds = append(proxyOutbounds, filterGroupTags...)

	outbounds = append(outbounds, Outbound{
		"tag":       "Proxy",
		"type":      "selector",
		"outbounds": proxyOutbounds,
		"default":   "Auto",
	})

	// 为启用的规则组创建选择器
	for _, rg := range b.ruleGroups {
		if !rg.Enabled {
			continue
		}

		var selectorOutbounds []string

		// 根据规则组的默认出站类型决定可选项
		if rg.Outbound == "DIRECT" || rg.Outbound == "REJECT" {
			// 直连/拦截规则组：只提供基础选项
			selectorOutbounds = []string{"DIRECT", "REJECT", "Proxy"}
		} else {
			// 代理规则组：提供完整选项（但不包含单节点）
			selectorOutbounds = []string{"Proxy", "Auto", "DIRECT", "REJECT"}
			selectorOutbounds = append(selectorOutbounds, countryGroupTags...) // 添加国家分组
			selectorOutbounds = append(selectorOutbounds, filterGroupTags...)
		}

		outbounds = append(outbounds, Outbound{
			"tag":       rg.Name,
			"type":      "selector",
			"outbounds": selectorOutbounds,
			"default":   rg.Outbound,
		})
	}

	// 创建漏网规则选择器
	fallbackOutbounds := []string{"Proxy", "DIRECT"}
	fallbackOutbounds = append(fallbackOutbounds, countryGroupTags...) // 添加国家分组
	fallbackOutbounds = append(fallbackOutbounds, filterGroupTags...)
	outbounds = append(outbounds, Outbound{
		"tag":       "Final",
		"type":      "selector",
		"outbounds": fallbackOutbounds,
		"default":   b.settings.FinalOutbound,
	})

	return outbounds
}

// nodeToOutbound 将节点转换为出站配置
func (b *ConfigBuilder) nodeToOutbound(node storage.Node) Outbound {
	outbound := Outbound{
		"tag":         node.Tag,
		"type":        node.Type,
		"server":      node.Server,
		"server_port": node.ServerPort,
	}

	// 复制 Extra 字段
	for k, v := range node.Extra {
		outbound[k] = v
	}

	if node.Type == "tuic" {
		b.ensureTUICOutboundTLS(outbound)
	}

	return outbound
}

func (b *ConfigBuilder) ensureTUICOutboundTLS(outbound Outbound) {
	tlsValue, exists := outbound["tls"]
	if !exists || tlsValue == nil {
		outbound["tls"] = map[string]interface{}{
			"enabled": true,
		}
		return
	}

	switch tls := tlsValue.(type) {
	case map[string]interface{}:
		tls["enabled"] = true
	case Outbound:
		tls["enabled"] = true
	default:
		outbound["tls"] = map[string]interface{}{
			"enabled": true,
		}
	}
}

// matchFilter 检查节点是否匹配过滤器
func (b *ConfigBuilder) matchFilter(node storage.Node, filter storage.Filter) bool {
	name := strings.ToLower(node.Tag)

	// 1. 检查国家包含条件
	if len(filter.IncludeCountries) > 0 {
		matched := false
		for _, country := range filter.IncludeCountries {
			if strings.EqualFold(node.Country, country) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 2. 检查国家排除条件
	for _, country := range filter.ExcludeCountries {
		if strings.EqualFold(node.Country, country) {
			return false
		}
	}

	// 3. 检查关键字包含条件
	if len(filter.Include) > 0 {
		matched := false
		for _, keyword := range filter.Include {
			if strings.Contains(name, strings.ToLower(keyword)) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 4. 检查关键字排除条件
	for _, keyword := range filter.Exclude {
		if strings.Contains(name, strings.ToLower(keyword)) {
			return false
		}
	}

	return true
}

// buildRoute 构建路由配置
func (b *ConfigBuilder) buildRoute() *RouteConfig {
	route := &RouteConfig{
		AutoDetectInterface: true,
		Final:               "Final",
		// 默认域名解析器：用于解析所有 outbound 的服务器地址，避免 DNS 循环
		DefaultDomainResolver: &DomainResolver{
			Server:     "dns_direct",
			RewriteTTL: 60,
		},
	}
	if b.dnsBypass() {
		route.Final = b.settings.FinalOutbound
	}

	if b.settings.DeploymentRole == "gateway" {
		route.AutoDetectInterface = false
		route.DefaultInterface = b.settings.Gateway.UplinkInterface
		if route.DefaultInterface == "" {
			route.DefaultInterface = b.settings.Gateway.LANInterface
		}
	}
	// 构建规则集
	ruleSetMap := make(map[string]bool)
	var ruleSets []RuleSet

	// 从规则组收集需要的规则集
	for _, rg := range b.ruleGroups {
		if !rg.Enabled {
			continue
		}
		for _, sr := range rg.SiteRules {
			tag := fmt.Sprintf("geosite-%s", sr)
			if !ruleSetMap[tag] {
				ruleSetMap[tag] = true
				ruleSets = append(ruleSets, RuleSet{
					Tag:            tag,
					Type:           "remote",
					Format:         "binary",
					URL:            b.buildRuleSetURL(fmt.Sprintf("%s/geosite-%s.srs", b.settings.RuleSetBaseURL, sr)),
					DownloadDetour: "DIRECT",
				})
			}
		}
		for _, ir := range rg.IPRules {
			tag := fmt.Sprintf("geoip-%s", ir)
			if !ruleSetMap[tag] {
				ruleSetMap[tag] = true
				ruleSets = append(ruleSets, RuleSet{
					Tag:            tag,
					Type:           "remote",
					Format:         "binary",
					URL:            b.buildRuleSetURL(fmt.Sprintf("%s/../rule-set-geoip/geoip-%s.srs", b.settings.RuleSetBaseURL, ir)),
					DownloadDetour: "DIRECT",
				})
			}
		}
	}

	// 从自定义规则收集需要的规则集
	for _, rule := range b.rules {
		if !rule.Enabled {
			continue
		}
		if rule.RuleType == "geosite" {
			for _, v := range rule.Values {
				tag := fmt.Sprintf("geosite-%s", v)
				if !ruleSetMap[tag] {
					ruleSetMap[tag] = true
					ruleSets = append(ruleSets, RuleSet{
						Tag:            tag,
						Type:           "remote",
						Format:         "binary",
						URL:            b.buildRuleSetURL(fmt.Sprintf("%s/geosite-%s.srs", b.settings.RuleSetBaseURL, v)),
						DownloadDetour: "DIRECT",
					})
				}
			}
		} else if rule.RuleType == "geoip" {
			for _, v := range rule.Values {
				tag := fmt.Sprintf("geoip-%s", v)
				if !ruleSetMap[tag] {
					ruleSetMap[tag] = true
					ruleSets = append(ruleSets, RuleSet{
						Tag:            tag,
						Type:           "remote",
						Format:         "binary",
						URL:            b.buildRuleSetURL(fmt.Sprintf("%s/../rule-set-geoip/geoip-%s.srs", b.settings.RuleSetBaseURL, v)),
						DownloadDetour: "DIRECT",
					})
				}
			}
		}
	}

	route.RuleSet = ruleSets

	// 构建路由规则
	var rules []RouteRule

	// 1. 添加 sniff action（嗅探流量类型，配合 FakeIP 使用）
	rules = append(rules, RouteRule{
		"action": "sniff",

		"timeout": "500ms",
	})

	// 2. DNS 劫持使用 action（替代已弃用的 dns-out）
	rules = append(rules, RouteRule{
		"protocol": "dns",
		"action":   "hijack-dns",
	})

	if b.settings.DeploymentRole == "gateway" {
		rules = append(rules, RouteRule{"inbound": []string{"lan-dns"}, "action": "hijack-dns"})
		rules = append(rules, b.deviceRouteRules()...)
	}
	// 3. 添加 hosts 域名的路由规则（优先级高，在其他规则之前）
	// 使用 override_address 直接指定目标 IP，避免 DIRECT outbound 重新 DNS 解析
	// 这解决了 sniff_override_destination 导致的 NXDOMAIN 问题
	systemHosts := b.systemHosts()
	for _, h := range b.settings.Hosts {
		if h.Enabled && h.Domain != "" && len(h.IPs) > 0 {
			systemHosts[h.Domain] = h.IPs
		}
	}
	keys := make([]string, 0, len(systemHosts))
	for domain := range systemHosts {
		keys = append(keys, domain)
	}
	sort.Strings(keys)
	for _, domain := range keys {
		ips := systemHosts[domain]
		if len(ips) > 0 {
			rules = append(rules, RouteRule{
				"domain":           []string{domain},
				"outbound":         "DIRECT",
				"override_address": ips[0],
			})
		}
	}

	// 按优先级排序自定义规则
	sortedRules := make([]storage.Rule, len(b.rules))
	copy(sortedRules, b.rules)
	sort.SliceStable(sortedRules, func(i, j int) bool {
		return sortedRules[i].Priority < sortedRules[j].Priority
	})

	// 添加自定义规则
	for _, rule := range sortedRules {
		if !rule.Enabled {
			continue
		}

		routeRule := ruleMatch(rule)
		routeRule["outbound"] = rule.Outbound

		switch rule.RuleType {
		case "port_range":
			routeRule["port_range"] = rule.Values
		case "process_name":
			routeRule["process_name"] = rule.Values
		case "domain_suffix":
			routeRule["domain_suffix"] = rule.Values
		case "domain_keyword":
			routeRule["domain_keyword"] = rule.Values
		case "domain":
			routeRule["domain"] = rule.Values
		case "ip_cidr":
			routeRule["ip_cidr"] = rule.Values
		case "port":
			// 将端口字符串转换为整数
			var ports []uint16
			for _, v := range rule.Values {
				if port, err := strconv.ParseUint(v, 10, 16); err == nil {
					ports = append(ports, uint16(port))
				}
			}
			if len(ports) == 1 {
				routeRule["port"] = ports[0]
			} else if len(ports) > 1 {
				routeRule["port"] = ports
			}
		case "geosite":
			var tags []string
			for _, v := range rule.Values {
				tags = append(tags, fmt.Sprintf("geosite-%s", v))
			}
			routeRule["rule_set"] = tags
		case "geoip":
			var tags []string
			for _, v := range rule.Values {
				tags = append(tags, fmt.Sprintf("geoip-%s", v))
			}
			routeRule["rule_set"] = tags
		}

		rules = append(rules, routeRule)
	}

	// 添加规则组的路由规则
	for _, rg := range b.ruleGroups {
		if !rg.Enabled {
			continue
		}

		// Site 规则
		outbound := rg.Name
		if b.dnsBypass() {
			outbound = rg.Outbound
		}
		if len(rg.SiteRules) > 0 {
			var tags []string
			for _, sr := range rg.SiteRules {
				tags = append(tags, fmt.Sprintf("geosite-%s", sr))
			}
			rules = append(rules, RouteRule{
				"rule_set": tags,
				"outbound": outbound,
			})
		}

		// IP 规则
		if len(rg.IPRules) > 0 {
			var tags []string
			for _, ir := range rg.IPRules {
				tags = append(tags, fmt.Sprintf("geoip-%s", ir))
			}
			rules = append(rules, RouteRule{
				"rule_set": tags,
				"outbound": outbound,
			})
		}
	}

	route.Rules = rules

	return route
}

// buildExperimental 构建实验性配置
func (b *ConfigBuilder) buildExperimental() *ExperimentalConfig {
	// 内置面板通过管理后端访问控制接口，独立于 mixed 的 LAN 开关。
	listenAddr := "127.0.0.1"
	secret := b.settings.ClashAPISecret

	return &ExperimentalConfig{
		ClashAPI: &ClashAPIConfig{
			ExternalController: fmt.Sprintf("%s:%d", listenAddr, b.settings.ClashAPIPort),
			Secret:             secret,
			DefaultMode:        "rule",
		},
		CacheFile: &CacheFileConfig{
			Enabled:     true,
			Path:        "cache.db",
			StoreFakeIP: true, // 持久化 FakeIP 映射，避免重启后地址变化
		},
	}
}
