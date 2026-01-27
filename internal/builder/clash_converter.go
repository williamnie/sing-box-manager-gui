package builder

import (
	"fmt"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/storage"
	"gopkg.in/yaml.v3"
)

// ClashConfig Clash 配置结构
type ClashConfig struct {
	Port               int                      `yaml:"port,omitempty"`
	SocksPort          int                      `yaml:"socks-port,omitempty"`
	MixedPort          int                      `yaml:"mixed-port,omitempty"`
	AllowLAN           bool                     `yaml:"allow-lan"`
	Mode               string                   `yaml:"mode"`
	LogLevel           string                   `yaml:"log-level"`
	ExternalController string                   `yaml:"external-controller,omitempty"`
	Secret             string                   `yaml:"secret,omitempty"`
	DNS                *ClashDNS                `yaml:"dns,omitempty"`
	TUN                *ClashTUN                `yaml:"tun,omitempty"`
	Proxies            []map[string]interface{} `yaml:"proxies"`
	ProxyGroups        []ClashProxyGroup        `yaml:"proxy-groups"`
	Rules              []string                 `yaml:"rules"`
}

// ClashDNS Clash DNS 配置
type ClashDNS struct {
	Enable            bool     `yaml:"enable"`
	Listen            string   `yaml:"listen,omitempty"`
	EnhancedMode      string   `yaml:"enhanced-mode,omitempty"`
	FakeIPRange       string   `yaml:"fake-ip-range,omitempty"`
	FakeIPFilter      []string `yaml:"fake-ip-filter,omitempty"`
	DefaultNameserver []string `yaml:"default-nameserver,omitempty"`
	Nameserver        []string `yaml:"nameserver,omitempty"`
	Fallback          []string `yaml:"fallback,omitempty"`
	FallbackFilter    *ClashFallbackFilter `yaml:"fallback-filter,omitempty"`
}

// ClashFallbackFilter Clash DNS fallback 过滤器
type ClashFallbackFilter struct {
	GeoIP     bool     `yaml:"geoip"`
	GeoIPCode string   `yaml:"geoip-code,omitempty"`
	IPCIDR    []string `yaml:"ipcidr,omitempty"`
	Domain    []string `yaml:"domain,omitempty"`
}

// ClashTUN Clash TUN 配置
type ClashTUN struct {
	Enable              bool     `yaml:"enable"`
	Stack               string   `yaml:"stack,omitempty"`
	DNSHijack           []string `yaml:"dns-hijack,omitempty"`
	AutoRoute           bool     `yaml:"auto-route"`
	AutoDetectInterface bool     `yaml:"auto-detect-interface"`
}

// ClashProxyGroup Clash 代理组
type ClashProxyGroup struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	Proxies   []string `yaml:"proxies"`
	URL       string   `yaml:"url,omitempty"`
	Interval  int      `yaml:"interval,omitempty"`
	Tolerance int      `yaml:"tolerance,omitempty"`
}

// ClashConverter Clash 配置转换器
type ClashConverter struct {
	settings   *storage.Settings
	nodes      []storage.Node
	filters    []storage.Filter
	rules      []storage.Rule
	ruleGroups []storage.RuleGroup
}

// NewClashConverter 创建 Clash 转换器
func NewClashConverter(settings *storage.Settings, nodes []storage.Node, filters []storage.Filter, rules []storage.Rule, ruleGroups []storage.RuleGroup) *ClashConverter {
	return &ClashConverter{
		settings:   settings,
		nodes:      nodes,
		filters:    filters,
		rules:      rules,
		ruleGroups: ruleGroups,
	}
}

// Convert 转换为 Clash 配置
func (c *ClashConverter) Convert() (*ClashConfig, error) {
	config := &ClashConfig{
		MixedPort:          c.settings.MixedPort,
		AllowLAN:           c.settings.AllowLAN,
		Mode:               "rule",
		LogLevel:           "info",
		ExternalController: fmt.Sprintf("0.0.0.0:%d", c.settings.ClashAPIPort),
	}

	if c.settings.AllowLAN && c.settings.ClashAPISecret != "" {
		config.Secret = c.settings.ClashAPISecret
	}

	// DNS 配置
	config.DNS = c.buildDNS()

	// TUN 配置
	if c.settings.TunEnabled {
		config.TUN = &ClashTUN{
			Enable:              true,
			Stack:               "system",
			DNSHijack:           []string{"any:53"},
			AutoRoute:           true,
			AutoDetectInterface: true,
		}
	}

	// 转换节点
	config.Proxies = c.convertProxies()

	// 转换代理组
	config.ProxyGroups = c.convertProxyGroups()

	// 转换规则
	config.Rules = c.convertRules()

	return config, nil
}

// ConvertYAML 转换为 YAML 字符串
func (c *ClashConverter) ConvertYAML() (string, error) {
	config, err := c.Convert()
	if err != nil {
		return "", err
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("序列化 Clash 配置失败: %w", err)
	}

	return string(data), nil
}

// buildDNS 构建 DNS 配置
func (c *ClashConverter) buildDNS() *ClashDNS {
	return &ClashDNS{
		Enable:            true,
		EnhancedMode:      "fake-ip",
		FakeIPRange:       "198.18.0.1/16",
		FakeIPFilter:      []string{"*.lan", "*.local", "localhost"},
		DefaultNameserver: []string{"223.5.5.5", "119.29.29.29"},
		Nameserver:        []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"},
		Fallback:          []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"},
		FallbackFilter: &ClashFallbackFilter{
			GeoIP:     true,
			GeoIPCode: "CN",
			IPCIDR:    []string{"240.0.0.0/4"},
		},
	}
}

// convertProxies 转换节点
func (c *ClashConverter) convertProxies() []map[string]interface{} {
	var proxies []map[string]interface{}

	for _, node := range c.nodes {
		proxy := c.nodeToClashProxy(node)
		if proxy != nil {
			proxies = append(proxies, proxy)
		}
	}

	return proxies
}

// nodeToClashProxy 将节点转换为 Clash 代理格式
func (c *ClashConverter) nodeToClashProxy(node storage.Node) map[string]interface{} {
	proxy := map[string]interface{}{
		"name":   node.Tag,
		"server": node.Server,
		"port":   node.ServerPort,
	}

	switch node.Type {
	case "shadowsocks":
		proxy["type"] = "ss"
		if method, ok := node.Extra["method"].(string); ok {
			proxy["cipher"] = method
		}
		if password, ok := node.Extra["password"].(string); ok {
			proxy["password"] = password
		}
		// UDP 支持
		proxy["udp"] = true

	case "vmess":
		proxy["type"] = "vmess"
		if uuid, ok := node.Extra["uuid"].(string); ok {
			proxy["uuid"] = uuid
		}
		if alterId, ok := node.Extra["alter_id"].(float64); ok {
			proxy["alterId"] = int(alterId)
		} else {
			proxy["alterId"] = 0
		}
		proxy["cipher"] = "auto"
		// TLS
		if tls, ok := node.Extra["tls"].(map[string]interface{}); ok {
			if enabled, ok := tls["enabled"].(bool); ok && enabled {
				proxy["tls"] = true
				if sni, ok := tls["server_name"].(string); ok {
					proxy["servername"] = sni
				}
				if insecure, ok := tls["insecure"].(bool); ok {
					proxy["skip-cert-verify"] = insecure
				}
			}
		}
		// Transport
		if transport, ok := node.Extra["transport"].(map[string]interface{}); ok {
			if tType, ok := transport["type"].(string); ok {
				proxy["network"] = tType
				if tType == "ws" {
					wsOpts := map[string]interface{}{}
					if path, ok := transport["path"].(string); ok {
						wsOpts["path"] = path
					}
					if headers, ok := transport["headers"].(map[string]interface{}); ok {
						wsOpts["headers"] = headers
					}
					proxy["ws-opts"] = wsOpts
				} else if tType == "grpc" {
					grpcOpts := map[string]interface{}{}
					if serviceName, ok := transport["service_name"].(string); ok {
						grpcOpts["grpc-service-name"] = serviceName
					}
					proxy["grpc-opts"] = grpcOpts
				}
			}
		}
		proxy["udp"] = true

	case "vless":
		proxy["type"] = "vless"
		if uuid, ok := node.Extra["uuid"].(string); ok {
			proxy["uuid"] = uuid
		}
		if flow, ok := node.Extra["flow"].(string); ok && flow != "" {
			proxy["flow"] = flow
		}
		// TLS
		if tls, ok := node.Extra["tls"].(map[string]interface{}); ok {
			if enabled, ok := tls["enabled"].(bool); ok && enabled {
				proxy["tls"] = true
				if sni, ok := tls["server_name"].(string); ok {
					proxy["servername"] = sni
				}
				if insecure, ok := tls["insecure"].(bool); ok {
					proxy["skip-cert-verify"] = insecure
				}
				// Reality
				if reality, ok := tls["reality"].(map[string]interface{}); ok {
					if enabled, ok := reality["enabled"].(bool); ok && enabled {
						realityOpts := map[string]interface{}{}
						if publicKey, ok := reality["public_key"].(string); ok {
							realityOpts["public-key"] = publicKey
						}
						if shortId, ok := reality["short_id"].(string); ok {
							realityOpts["short-id"] = shortId
						}
						proxy["reality-opts"] = realityOpts
					}
				}
				// UTLS
				if utls, ok := tls["utls"].(map[string]interface{}); ok {
					if fingerprint, ok := utls["fingerprint"].(string); ok {
						proxy["client-fingerprint"] = fingerprint
					}
				}
			}
		}
		// Transport
		if transport, ok := node.Extra["transport"].(map[string]interface{}); ok {
			if tType, ok := transport["type"].(string); ok {
				proxy["network"] = tType
				if tType == "ws" {
					wsOpts := map[string]interface{}{}
					if path, ok := transport["path"].(string); ok {
						wsOpts["path"] = path
					}
					if headers, ok := transport["headers"].(map[string]interface{}); ok {
						wsOpts["headers"] = headers
					}
					proxy["ws-opts"] = wsOpts
				} else if tType == "grpc" {
					grpcOpts := map[string]interface{}{}
					if serviceName, ok := transport["service_name"].(string); ok {
						grpcOpts["grpc-service-name"] = serviceName
					}
					proxy["grpc-opts"] = grpcOpts
				}
			}
		}
		proxy["udp"] = true

	case "trojan":
		proxy["type"] = "trojan"
		if password, ok := node.Extra["password"].(string); ok {
			proxy["password"] = password
		}
		// TLS
		if tls, ok := node.Extra["tls"].(map[string]interface{}); ok {
			if sni, ok := tls["server_name"].(string); ok {
				proxy["sni"] = sni
			}
			if insecure, ok := tls["insecure"].(bool); ok {
				proxy["skip-cert-verify"] = insecure
			}
		}
		// Transport
		if transport, ok := node.Extra["transport"].(map[string]interface{}); ok {
			if tType, ok := transport["type"].(string); ok {
				proxy["network"] = tType
				if tType == "ws" {
					wsOpts := map[string]interface{}{}
					if path, ok := transport["path"].(string); ok {
						wsOpts["path"] = path
					}
					if headers, ok := transport["headers"].(map[string]interface{}); ok {
						wsOpts["headers"] = headers
					}
					proxy["ws-opts"] = wsOpts
				} else if tType == "grpc" {
					grpcOpts := map[string]interface{}{}
					if serviceName, ok := transport["service_name"].(string); ok {
						grpcOpts["grpc-service-name"] = serviceName
					}
					proxy["grpc-opts"] = grpcOpts
				}
			}
		}
		proxy["udp"] = true

	case "hysteria2":
		proxy["type"] = "hysteria2"
		if password, ok := node.Extra["password"].(string); ok {
			proxy["password"] = password
		}
		// TLS
		if tls, ok := node.Extra["tls"].(map[string]interface{}); ok {
			if sni, ok := tls["server_name"].(string); ok {
				proxy["sni"] = sni
			}
			if insecure, ok := tls["insecure"].(bool); ok {
				proxy["skip-cert-verify"] = insecure
			}
		}
		// Obfs
		if obfs, ok := node.Extra["obfs"].(map[string]interface{}); ok {
			if obfsType, ok := obfs["type"].(string); ok {
				proxy["obfs"] = obfsType
			}
			if obfsPassword, ok := obfs["password"].(string); ok {
				proxy["obfs-password"] = obfsPassword
			}
		}

	case "tuic":
		proxy["type"] = "tuic"
		if uuid, ok := node.Extra["uuid"].(string); ok {
			proxy["uuid"] = uuid
		}
		if password, ok := node.Extra["password"].(string); ok {
			proxy["password"] = password
		}
		if congestion, ok := node.Extra["congestion_control"].(string); ok {
			proxy["congestion-controller"] = congestion
		}
		// TLS
		if tls, ok := node.Extra["tls"].(map[string]interface{}); ok {
			if sni, ok := tls["server_name"].(string); ok {
				proxy["sni"] = sni
			}
			if insecure, ok := tls["insecure"].(bool); ok {
				proxy["skip-cert-verify"] = insecure
			}
			if alpn, ok := tls["alpn"].([]interface{}); ok {
				var alpnList []string
				for _, a := range alpn {
					if s, ok := a.(string); ok {
						alpnList = append(alpnList, s)
					}
				}
				proxy["alpn"] = alpnList
			}
		}

	default:
		return nil
	}

	return proxy
}

// convertProxyGroups 转换代理组
func (c *ClashConverter) convertProxyGroups() []ClashProxyGroup {
	var groups []ClashProxyGroup
	var allNodeTags []string
	nodeTagSet := make(map[string]bool)
	countryNodes := make(map[string][]string)

	// 收集所有节点标签
	for _, node := range c.nodes {
		tag := node.Tag
		if !nodeTagSet[tag] {
			allNodeTags = append(allNodeTags, tag)
			nodeTagSet[tag] = true
		}
		if node.Country != "" {
			countryNodes[node.Country] = append(countryNodes[node.Country], tag)
		} else {
			countryNodes["OTHER"] = append(countryNodes["OTHER"], tag)
		}
	}

	// 收集过滤器分组
	var filterGroupTags []string
	for _, filter := range c.filters {
		if !filter.Enabled {
			continue
		}

		var filteredTags []string
		for _, node := range c.nodes {
			if c.matchFilter(node, filter) {
				filteredTags = append(filteredTags, node.Tag)
			}
		}

		if len(filteredTags) == 0 {
			continue
		}

		groupTag := filter.Name
		filterGroupTags = append(filterGroupTags, groupTag)

		group := ClashProxyGroup{
			Name:    groupTag,
			Proxies: filteredTags,
		}

		if filter.Mode == "urltest" {
			group.Type = "url-test"
			group.URL = "https://www.gstatic.com/generate_204"
			group.Interval = 300
			group.Tolerance = 50
			if filter.URLTestConfig != nil {
				group.URL = filter.URLTestConfig.URL
				group.Tolerance = filter.URLTestConfig.Tolerance
			}
		} else {
			group.Type = "select"
		}

		groups = append(groups, group)
	}

	// 创建国家分组
	var countryGroupTags []string
	for code, nodes := range countryNodes {
		if len(nodes) == 0 {
			continue
		}

		emoji := storage.GetCountryEmoji(code)
		name := storage.GetCountryName(code)
		groupTag := fmt.Sprintf("%s %s", emoji, name)
		countryGroupTags = append(countryGroupTags, groupTag)

		groups = append(groups, ClashProxyGroup{
			Name:      groupTag,
			Type:      "url-test",
			Proxies:   nodes,
			URL:       "https://www.gstatic.com/generate_204",
			Interval:  300,
			Tolerance: 50,
		})
	}

	// 创建 Auto 组
	if len(allNodeTags) > 0 {
		groups = append(groups, ClashProxyGroup{
			Name:      "Auto",
			Type:      "url-test",
			Proxies:   allNodeTags,
			URL:       "https://www.gstatic.com/generate_204",
			Interval:  300,
			Tolerance: 50,
		})
	}

	// 创建 Proxy 主选择器
	proxyOutbounds := []string{"Auto"}
	proxyOutbounds = append(proxyOutbounds, countryGroupTags...)
	proxyOutbounds = append(proxyOutbounds, filterGroupTags...)

	groups = append([]ClashProxyGroup{{
		Name:    "Proxy",
		Type:    "select",
		Proxies: proxyOutbounds,
	}}, groups...)

	// 为规则组创建选择器
	for _, rg := range c.ruleGroups {
		if !rg.Enabled {
			continue
		}

		var selectorOutbounds []string
		if rg.Outbound == "DIRECT" || rg.Outbound == "REJECT" {
			selectorOutbounds = []string{"DIRECT", "REJECT", "Proxy"}
		} else {
			selectorOutbounds = []string{"Proxy", "Auto", "DIRECT"}
			selectorOutbounds = append(selectorOutbounds, countryGroupTags...)
		}

		groups = append(groups, ClashProxyGroup{
			Name:    rg.Name,
			Type:    "select",
			Proxies: selectorOutbounds,
		})
	}

	// Final 组
	groups = append(groups, ClashProxyGroup{
		Name:    "Final",
		Type:    "select",
		Proxies: []string{"Proxy", "DIRECT"},
	})

	return groups
}

// convertRules 转换规则
func (c *ClashConverter) convertRules() []string {
	var rules []string

	// 添加自定义规则
	for _, rule := range c.rules {
		if !rule.Enabled {
			continue
		}

		for _, value := range rule.Values {
			var clashRule string
			switch rule.RuleType {
			case "domain":
				clashRule = fmt.Sprintf("DOMAIN,%s,%s", value, rule.Outbound)
			case "domain_suffix":
				clashRule = fmt.Sprintf("DOMAIN-SUFFIX,%s,%s", value, rule.Outbound)
			case "domain_keyword":
				clashRule = fmt.Sprintf("DOMAIN-KEYWORD,%s,%s", value, rule.Outbound)
			case "ip_cidr":
				clashRule = fmt.Sprintf("IP-CIDR,%s,%s,no-resolve", value, rule.Outbound)
			case "geosite":
				clashRule = fmt.Sprintf("GEOSITE,%s,%s", value, rule.Outbound)
			case "geoip":
				clashRule = fmt.Sprintf("GEOIP,%s,%s", value, rule.Outbound)
			case "port":
				clashRule = fmt.Sprintf("DST-PORT,%s,%s", value, rule.Outbound)
			}
			if clashRule != "" {
				rules = append(rules, clashRule)
			}
		}
	}

	// 添加规则组的规则
	for _, rg := range c.ruleGroups {
		if !rg.Enabled {
			continue
		}

		for _, sr := range rg.SiteRules {
			rules = append(rules, fmt.Sprintf("GEOSITE,%s,%s", sr, rg.Name))
		}
		for _, ir := range rg.IPRules {
			rules = append(rules, fmt.Sprintf("GEOIP,%s,%s", ir, rg.Name))
		}
	}

	// 最终规则
	rules = append(rules, "MATCH,Final")

	return rules
}

// matchFilter 检查节点是否匹配过滤器
func (c *ClashConverter) matchFilter(node storage.Node, filter storage.Filter) bool {
	name := strings.ToLower(node.Tag)

	// 检查国家包含条件
	if len(filter.IncludeCountries) > 0 {
		matched := false
		for _, country := range filter.IncludeCountries {
			if node.Country == country {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// 检查国家排除条件
	for _, country := range filter.ExcludeCountries {
		if node.Country == country {
			return false
		}
	}

	// 检查关键字包含条件
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

	// 检查关键字排除条件
	for _, keyword := range filter.Exclude {
		if strings.Contains(name, strings.ToLower(keyword)) {
			return false
		}
	}

	return true
}
