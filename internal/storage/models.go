package storage

import "time"

// Subscription 订阅
type Subscription struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	NodeCount int        `json:"node_count"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpireAt  *time.Time `json:"expire_at,omitempty"`
	Traffic   *Traffic   `json:"traffic,omitempty"`
	Nodes     []Node     `json:"nodes"`
	Enabled   bool       `json:"enabled"`
}

// Traffic 流量信息
type Traffic struct {
	Total     int64 `json:"total"`     // 总流量 (bytes)
	Used      int64 `json:"used"`      // 已用流量
	Remaining int64 `json:"remaining"` // 剩余流量
}

// Node 节点
type Node struct {
	Tag          string                 `json:"tag"`
	Type         string                 `json:"type"`                    // shadowsocks/vmess/vless/trojan/hysteria2/tuic
	Server       string                 `json:"server"`
	ServerPort   int                    `json:"server_port"`
	Extra        map[string]interface{} `json:"extra,omitempty"`         // 协议特定字段
	Country      string                 `json:"country,omitempty"`       // 国家代码
	CountryEmoji string                 `json:"country_emoji,omitempty"` // 国家 emoji
	Disabled     bool                   `json:"disabled,omitempty"`      // 是否禁用
}

// ManualNode 手动添加的节点
type ManualNode struct {
	ID      string `json:"id"`
	Node    Node   `json:"node"`
	Enabled bool   `json:"enabled"`
}

// CountryGroup 国家节点分组
type CountryGroup struct {
	Code      string `json:"code"`       // 国家代码 (如 HK, US, JP)
	Name      string `json:"name"`       // 国家名称 (如 香港, 美国, 日本)
	Emoji     string `json:"emoji"`      // 国旗 emoji
	NodeCount int    `json:"node_count"` // 节点数量
}

// Filter 过滤器
type Filter struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Include          []string       `json:"include"`           // 包含关键字
	Exclude          []string       `json:"exclude"`           // 排除关键字
	IncludeCountries []string       `json:"include_countries"` // 包含的国家代码
	ExcludeCountries []string       `json:"exclude_countries"` // 排除的国家代码
	Mode             string         `json:"mode"`              // urltest / select
	URLTestConfig    *URLTestConfig `json:"urltest_config,omitempty"`
	Subscriptions    []string       `json:"subscriptions"` // 适用的订阅ID，空表示全部
	AllNodes         bool           `json:"all_nodes"`     // 是否应用于所有节点
	Enabled          bool           `json:"enabled"`
}

// URLTestConfig urltest 模式配置
type URLTestConfig struct {
	URL       string `json:"url"`
	Interval  string `json:"interval"`
	Tolerance int    `json:"tolerance"`
}

// Rule 自定义规则
type Rule struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	RuleType string   `json:"rule_type"` // domain_suffix/domain_keyword/ip_cidr/geosite/geoip/port
	Values   []string `json:"values"`    // 规则值列表
	Outbound string   `json:"outbound"`  // 目标出站
	Enabled  bool     `json:"enabled"`
	Priority int      `json:"priority"`  // 优先级 (越小越优先)
}

// RuleGroup 预设规则组
type RuleGroup struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	SiteRules []string `json:"site_rules"` // geosite 规则
	IPRules   []string `json:"ip_rules"`   // geoip 规则
	Outbound  string   `json:"outbound"`
	Enabled   bool     `json:"enabled"`
}

// HostEntry DNS hosts 映射条目
type HostEntry struct {
	ID      string   `json:"id"`
	Domain  string   `json:"domain"` // 域名
	IPs     []string `json:"ips"`    // IP 地址列表
	Enabled bool     `json:"enabled"`
}

// Settings 全局设置
type Settings struct {
	// sing-box 路径
	SingBoxPath string `json:"singbox_path"`
	ConfigPath  string `json:"config_path"`

	// 入站配置
	MixedPort  int  `json:"mixed_port"`  // HTTP/SOCKS5 混合端口
	TunEnabled bool `json:"tun_enabled"` // TUN 模式
	AllowLAN   bool `json:"allow_lan"`   // 允许局域网访问

	// DNS 配置
	ProxyDNS  string      `json:"proxy_dns"`        // 代理 DNS
	DirectDNS string      `json:"direct_dns"`       // 直连 DNS
	Hosts     []HostEntry `json:"hosts,omitempty"`  // DNS hosts 映射

	// 控制面板
	WebPort        int    `json:"web_port"`          // 管理界面端口
	ClashAPIPort   int    `json:"clash_api_port"`    // Clash API 端口
	ClashUIPath    string `json:"clash_ui_path"`     // zashboard 路径
	ClashAPISecret string `json:"clash_api_secret"`  // ClashAPI 密钥

	// 漏网规则
	FinalOutbound string `json:"final_outbound"` // 默认出站

	// 规则集源
	RuleSetBaseURL string `json:"ruleset_base_url"` // 规则集下载地址

	// 自动化设置
	AutoApply            bool `json:"auto_apply"`            // 配置变更后自动应用
	SubscriptionInterval int  `json:"subscription_interval"` // 订阅自动更新间隔 (分钟)，0 表示禁用

	// 健康检查设置
	HealthCheckEnabled  bool `json:"health_check_enabled"`  // 启用健康检查
	HealthCheckInterval int  `json:"health_check_interval"` // 健康检查间隔 (秒)，默认 30
	AutoRestart         bool `json:"auto_restart"`          // 健康检查失败时自动重启

	// GitHub 代理设置
	GithubProxy string `json:"github_proxy"` // GitHub 代理地址，如 https://ghproxy.com/

	// 订阅下发配置
	SubscribeEnabled bool   `json:"subscribe_enabled"` // 是否启用订阅下发
	SubscribeToken   string `json:"subscribe_token"`   // 订阅访问令牌
	SubscribeURL     string `json:"subscribe_url"`     // 订阅地址 (覆盖自动生成的地址)
}

// DefaultSettings 默认设置
func DefaultSettings() *Settings {
	return &Settings{
		SingBoxPath:          "bin/sing-box",
		ConfigPath:           "generated/config.json",
		MixedPort:            2080,
		TunEnabled:           true,
		AllowLAN:             false, // 默认不允许局域网访问
		ProxyDNS:             "https://1.1.1.1/dns-query",
		DirectDNS:            "https://dns.alidns.com/dns-query",
		WebPort:              9090,
		ClashAPIPort:         9091,
		ClashUIPath:          "zashboard",
		ClashAPISecret:       "", // 默认为空，开启局域网时自动生成
		FinalOutbound:        "Proxy",
		RuleSetBaseURL:       "https://github.com/lyc8503/sing-box-rules/raw/rule-set-geosite",
		AutoApply:            true, // 默认开启自动应用
		SubscriptionInterval: 60,   // 默认 60 分钟更新一次
		HealthCheckEnabled:   true, // 默认开启健康检查
		HealthCheckInterval:  30,   // 默认 30 秒检查一次
		AutoRestart:          true, // 默认开启自动重启
		GithubProxy:          "",   // 默认不使用代理
		SubscribeEnabled:     false, // 默认不启用订阅下发
		SubscribeToken:       "",   // 默认无令牌
		SubscribeURL:         "",   // 默认无自定义地址
	}
}

// AppData 应用数据
type AppData struct {
	Subscriptions []Subscription `json:"subscriptions"`
	ManualNodes   []ManualNode   `json:"manual_nodes"`
	Filters       []Filter       `json:"filters"`
	Rules         []Rule         `json:"rules"`
	RuleGroups    []RuleGroup    `json:"rule_groups"`
	Settings      *Settings      `json:"settings"`
}

// DefaultRuleGroups 默认规则组
func DefaultRuleGroups() []RuleGroup {
	return []RuleGroup{
		{ID: "ad-block", Name: "广告拦截", SiteRules: []string{"category-ads-all"}, Outbound: "REJECT", Enabled: true},
		{ID: "ai-services", Name: "AI 服务", SiteRules: []string{"openai", "anthropic", "jetbrains-ai"}, Outbound: "Proxy", Enabled: true},
		{ID: "google", Name: "Google", SiteRules: []string{"google"}, IPRules: []string{"google"}, Outbound: "Proxy", Enabled: true},
		{ID: "youtube", Name: "YouTube", SiteRules: []string{"youtube"}, Outbound: "Proxy", Enabled: true},
		{ID: "github", Name: "GitHub", SiteRules: []string{"github"}, Outbound: "Proxy", Enabled: true},
		{ID: "telegram", Name: "Telegram", SiteRules: []string{"telegram"}, IPRules: []string{"telegram"}, Outbound: "Proxy", Enabled: true},
		{ID: "twitter", Name: "Twitter/X", SiteRules: []string{"twitter"}, Outbound: "Proxy", Enabled: true},
		{ID: "netflix", Name: "Netflix", SiteRules: []string{"netflix"}, Outbound: "Proxy", Enabled: false},
		{ID: "spotify", Name: "Spotify", SiteRules: []string{"spotify"}, Outbound: "Proxy", Enabled: false},
		{ID: "apple", Name: "Apple", SiteRules: []string{"apple"}, Outbound: "DIRECT", Enabled: true},
		{ID: "microsoft", Name: "Microsoft", SiteRules: []string{"microsoft"}, Outbound: "DIRECT", Enabled: true},
		{ID: "cn", Name: "中国地区", SiteRules: []string{"geolocation-cn"}, IPRules: []string{"cn"}, Outbound: "DIRECT", Enabled: true},
		{ID: "private", Name: "私有网络", SiteRules: []string{"private"}, IPRules: []string{"private"}, Outbound: "DIRECT", Enabled: true},
	}
}

// CountryNames 国家代码到中文名称的映射
var CountryNames = map[string]string{
	"HK": "香港",
	"TW": "台湾",
	"JP": "日本",
	"KR": "韩国",
	"SG": "新加坡",
	"US": "美国",
	"GB": "英国",
	"DE": "德国",
	"FR": "法国",
	"NL": "荷兰",
	"AU": "澳大利亚",
	"CA": "加拿大",
	"RU": "俄罗斯",
	"IN": "印度",
	"BR": "巴西",
	"AR": "阿根廷",
	"TR": "土耳其",
	"TH": "泰国",
	"VN": "越南",
	"MY": "马来西亚",
	"PH": "菲律宾",
	"ID": "印尼",
	"AE": "阿联酋",
	"ZA": "南非",
	"CH": "瑞士",
	"IT": "意大利",
	"ES": "西班牙",
	"SE": "瑞典",
	"NO": "挪威",
	"FI": "芬兰",
	"DK": "丹麦",
	"PL": "波兰",
	"CZ": "捷克",
	"AT": "奥地利",
	"IE": "爱尔兰",
	"PT": "葡萄牙",
	"GR": "希腊",
	"IL": "以色列",
	"MX": "墨西哥",
	"CL": "智利",
	"CO": "哥伦比亚",
	"PE": "秘鲁",
	"NZ":    "新西兰",
	"OTHER": "其他",
}

// CountryEmojis 国家代码到 emoji 的映射
var CountryEmojis = map[string]string{
	"HK": "🇭🇰",
	"TW": "🇹🇼",
	"JP": "🇯🇵",
	"KR": "🇰🇷",
	"SG": "🇸🇬",
	"US": "🇺🇸",
	"GB": "🇬🇧",
	"DE": "🇩🇪",
	"FR": "🇫🇷",
	"NL": "🇳🇱",
	"AU": "🇦🇺",
	"CA": "🇨🇦",
	"RU": "🇷🇺",
	"IN": "🇮🇳",
	"BR": "🇧🇷",
	"AR": "🇦🇷",
	"TR": "🇹🇷",
	"TH": "🇹🇭",
	"VN": "🇻🇳",
	"MY": "🇲🇾",
	"PH": "🇵🇭",
	"ID": "🇮🇩",
	"AE": "🇦🇪",
	"ZA": "🇿🇦",
	"CH": "🇨🇭",
	"IT": "🇮🇹",
	"ES": "🇪🇸",
	"SE": "🇸🇪",
	"NO": "🇳🇴",
	"FI": "🇫🇮",
	"DK": "🇩🇰",
	"PL": "🇵🇱",
	"CZ": "🇨🇿",
	"AT": "🇦🇹",
	"IE": "🇮🇪",
	"PT": "🇵🇹",
	"GR": "🇬🇷",
	"IL": "🇮🇱",
	"MX": "🇲🇽",
	"CL": "🇨🇱",
	"CO": "🇨🇴",
	"PE": "🇵🇪",
	"NZ":    "🇳🇿",
	"OTHER": "🌐",
}

// GetCountryName 获取国家名称
func GetCountryName(code string) string {
	if name, ok := CountryNames[code]; ok {
		return name
	}
	return code
}

// GetCountryEmoji 获取国家 emoji
func GetCountryEmoji(code string) string {
	if emoji, ok := CountryEmojis[code]; ok {
		return emoji
	}
	return "🌐"
}
