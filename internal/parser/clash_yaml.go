package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/storage"
	"github.com/xiaobei/singbox-manager/pkg/utils"
	"gopkg.in/yaml.v3"
)

// ClashConfig Clash 配置结构
type ClashConfig struct {
	Proxies []ClashProxy `yaml:"proxies"`
}

// ClashProxy Clash 代理配置
type ClashProxy struct {
	Name           string                 `yaml:"name"`
	Type           string                 `yaml:"type"`
	Server         string                 `yaml:"server"`
	Port           int                    `yaml:"port"`
	Password       string                 `yaml:"password,omitempty"`
	Username       string                 `yaml:"username,omitempty"` // SOCKS 用户名
	UUID           string                 `yaml:"uuid,omitempty"`
	Cipher         string                 `yaml:"cipher,omitempty"`
	AlterId        int                    `yaml:"alterId,omitempty"`
	Network        string                 `yaml:"network,omitempty"`
	TLS            bool                   `yaml:"tls,omitempty"`
	SkipCertVerify bool                   `yaml:"skip-cert-verify,omitempty"`
	SNI            string                 `yaml:"sni,omitempty"`
	Servername     string                 `yaml:"servername,omitempty"` // Clash 格式的 SNI 字段
	ALPN           []string               `yaml:"alpn,omitempty"`
	Fingerprint    string                 `yaml:"fingerprint,omitempty"`
	Flow           string                 `yaml:"flow,omitempty"`
	UDP            bool                   `yaml:"udp,omitempty"`
	Plugin         string                 `yaml:"plugin,omitempty"`
	PluginOpts     map[string]interface{} `yaml:"plugin-opts,omitempty"`
	WSOpts         *WSOpts                `yaml:"ws-opts,omitempty"`
	H2Opts         *H2Opts                `yaml:"h2-opts,omitempty"`
	HTTPOpts       *HTTPOpts              `yaml:"http-opts,omitempty"`
	GrpcOpts       *GrpcOpts              `yaml:"grpc-opts,omitempty"`
	RealityOpts    *RealityOpts           `yaml:"reality-opts,omitempty"`
	// Hysteria2 特有
	Auth         string `yaml:"auth,omitempty"`
	Obfs         string `yaml:"obfs,omitempty"`
	ObfsPassword string `yaml:"obfs-password,omitempty"`
	Up           string `yaml:"up,omitempty"`
	Down         string `yaml:"down,omitempty"`
	// TUIC 特有
	CongestionController string `yaml:"congestion-controller,omitempty"`
	UDPRelayMode         string `yaml:"udp-relay-mode,omitempty"`
	ReduceRTT            bool   `yaml:"reduce-rtt,omitempty"`
}

// WSOpts WebSocket 选项
type WSOpts struct {
	Path                string            `yaml:"path,omitempty"`
	Headers             map[string]string `yaml:"headers,omitempty"`
	MaxEarlyData        int               `yaml:"max-early-data,omitempty"`
	EarlyDataHeaderName string            `yaml:"early-data-header-name,omitempty"`
}

// H2Opts HTTP/2 选项
type H2Opts struct {
	Path string   `yaml:"path,omitempty"`
	Host []string `yaml:"host,omitempty"`
}

// HTTPOpts HTTP 选项
type HTTPOpts struct {
	Method  string              `yaml:"method,omitempty"`
	Path    []string            `yaml:"path,omitempty"`
	Headers map[string][]string `yaml:"headers,omitempty"`
}

// GrpcOpts gRPC 选项
type GrpcOpts struct {
	GrpcServiceName string `yaml:"grpc-service-name,omitempty"`
}

// RealityOpts Reality 选项
type RealityOpts struct {
	PublicKey string `yaml:"public-key,omitempty"`
	ShortID   string `yaml:"short-id,omitempty"`
}

// ParseClashYAML 解析 Clash YAML 配置
func ParseClashYAML(content string) ([]storage.Node, error) {
	var config ClashConfig
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return nil, fmt.Errorf("解析 YAML 失败: %w", err)
	}

	var nodes []storage.Node
	for _, proxy := range config.Proxies {
		node, err := convertClashProxy(proxy)
		if err != nil {
			continue // 跳过无法解析的节点
		}
		nodes = append(nodes, *node)
	}

	return nodes, nil
}

// convertClashProxy 转换 Clash 代理配置为内部格式
func convertClashProxy(proxy ClashProxy) (*storage.Node, error) {
	var nodeType string
	extra := make(map[string]interface{})

	switch strings.ToLower(proxy.Type) {
	case "ss", "shadowsocks":
		nodeType = "shadowsocks"
		extra["method"] = proxy.Cipher
		extra["password"] = proxy.Password
		if proxy.Plugin != "" {
			extra["plugin"] = proxy.Plugin
			if proxy.PluginOpts != nil {
				extra["plugin_opts"] = proxy.PluginOpts
			}
		}

	case "vmess":
		nodeType = "vmess"
		extra["uuid"] = proxy.UUID
		extra["alter_id"] = proxy.AlterId
		extra["security"] = proxy.Cipher
		if extra["security"] == "" {
			extra["security"] = "auto"
		}

	case "vless":
		nodeType = "vless"
		extra["uuid"] = proxy.UUID
		if proxy.Flow != "" {
			extra["flow"] = proxy.Flow
		}

	case "trojan":
		nodeType = "trojan"
		extra["password"] = proxy.Password

	case "hysteria2", "hy2":
		nodeType = "hysteria2"
		if proxy.Password != "" {
			extra["password"] = proxy.Password
		} else if proxy.Auth != "" {
			extra["password"] = proxy.Auth
		}
		if proxy.Obfs != "" && proxy.ObfsPassword != "" {
			extra["obfs"] = map[string]interface{}{
				"type":     proxy.Obfs,
				"password": proxy.ObfsPassword,
			}
		}
		// 带宽配置 - 转换为 up_mbps/down_mbps
		if proxy.Up != "" {
			if mbps := parseBandwidthClash(proxy.Up); mbps > 0 {
				extra["up_mbps"] = mbps
			}
		}
		if proxy.Down != "" {
			if mbps := parseBandwidthClash(proxy.Down); mbps > 0 {
				extra["down_mbps"] = mbps
			}
		}
		// Hysteria2 必须启用 TLS
		tls := map[string]interface{}{
			"enabled": true,
		}
		if proxy.SNI != "" {
			tls["server_name"] = proxy.SNI
		} else if proxy.Servername != "" {
			tls["server_name"] = proxy.Servername
		} else {
			tls["server_name"] = proxy.Server
		}
		if proxy.SkipCertVerify {
			tls["insecure"] = true
		}
		if len(proxy.ALPN) > 0 {
			tls["alpn"] = proxy.ALPN
		}
		extra["tls"] = tls

	case "tuic":
		nodeType = "tuic"
		extra["uuid"] = proxy.UUID
		extra["password"] = proxy.Password
		if proxy.CongestionController != "" {
			extra["congestion_control"] = proxy.CongestionController
		}
		if proxy.UDPRelayMode != "" {
			extra["udp_relay_mode"] = proxy.UDPRelayMode
		}
		if proxy.ReduceRTT {
			extra["zero_rtt_handshake"] = true
		}

	case "socks", "socks5":
		nodeType = "socks"
		extra["version"] = "5"
		if proxy.Username != "" {
			extra["username"] = proxy.Username
		}
		if proxy.Password != "" {
			extra["password"] = proxy.Password
		}

	case "socks4":
		nodeType = "socks"
		extra["version"] = "4"
		if proxy.Username != "" {
			extra["username"] = proxy.Username
		}

	default:
		return nil, fmt.Errorf("不支持的代理类型: %s", proxy.Type)
	}

	// 传输层配置
	network := proxy.Network
	if network == "" {
		network = "tcp"
	}

	if network != "tcp" || proxy.WSOpts != nil || proxy.H2Opts != nil || proxy.GrpcOpts != nil {
		transport := map[string]interface{}{
			"type": network,
		}

		switch network {
		case "ws":
			if proxy.WSOpts != nil {
				if proxy.WSOpts.Path != "" {
					transport["path"] = proxy.WSOpts.Path
				}
				if len(proxy.WSOpts.Headers) > 0 {
					transport["headers"] = proxy.WSOpts.Headers
				}
				if proxy.WSOpts.MaxEarlyData > 0 {
					transport["max_early_data"] = proxy.WSOpts.MaxEarlyData
				}
				if proxy.WSOpts.EarlyDataHeaderName != "" {
					transport["early_data_header_name"] = proxy.WSOpts.EarlyDataHeaderName
				}
			}
		case "h2":
			if proxy.H2Opts != nil {
				if proxy.H2Opts.Path != "" {
					transport["path"] = proxy.H2Opts.Path
				}
				if len(proxy.H2Opts.Host) > 0 {
					transport["host"] = proxy.H2Opts.Host
				}
			}
		case "http":
			if proxy.HTTPOpts != nil {
				if proxy.HTTPOpts.Method != "" {
					transport["method"] = proxy.HTTPOpts.Method
				}
				if len(proxy.HTTPOpts.Path) > 0 {
					transport["path"] = proxy.HTTPOpts.Path[0]
				}
				if len(proxy.HTTPOpts.Headers) > 0 {
					transport["headers"] = proxy.HTTPOpts.Headers
				}
			}
		case "grpc":
			if proxy.GrpcOpts != nil && proxy.GrpcOpts.GrpcServiceName != "" {
				transport["service_name"] = proxy.GrpcOpts.GrpcServiceName
			}
		}

		extra["transport"] = transport
	}

	// TLS 配置
	if proxy.TLS {
		tls := map[string]interface{}{
			"enabled": true,
		}

		// 设置 server_name（按优先级：SNI > Servername > 服务器地址）
		if proxy.SNI != "" {
			tls["server_name"] = proxy.SNI
		} else if proxy.Servername != "" {
			tls["server_name"] = proxy.Servername
		} else {
			// 回退到服务器地址，确保 TLS 握手有正确的 SNI
			tls["server_name"] = proxy.Server
		}

		if proxy.SkipCertVerify {
			tls["insecure"] = true
		}

		if len(proxy.ALPN) > 0 {
			tls["alpn"] = proxy.ALPN
		}

		if proxy.Fingerprint != "" {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": proxy.Fingerprint,
			}
		}

		// Reality 配置
		if proxy.RealityOpts != nil {
			reality := map[string]interface{}{
				"enabled": true,
			}
			if proxy.RealityOpts.PublicKey != "" {
				reality["public_key"] = proxy.RealityOpts.PublicKey
			}
			if proxy.RealityOpts.ShortID != "" {
				reality["short_id"] = proxy.RealityOpts.ShortID
			}
			tls["reality"] = reality

			// Reality 必须要有 uTLS 配置，如果没有设置则使用默认值
			if _, ok := tls["utls"]; !ok {
				fp := proxy.Fingerprint
				if fp == "" {
					fp = "chrome" // 默认使用 chrome fingerprint
				}
				tls["utls"] = map[string]interface{}{
					"enabled":     true,
					"fingerprint": fp,
				}
			}
		}

		extra["tls"] = tls
	}

	// 解析国家信息
	var country, countryEmoji string
	if countryInfo := utils.ParseCountryFromNodeName(proxy.Name); countryInfo != nil {
		country = utils.InternCountry(countryInfo.Code)
		countryEmoji = utils.InternEmoji(countryInfo.Emoji)
	}

	node := &storage.Node{
		Tag:          proxy.Name,
		Type:         nodeType,
		Server:       proxy.Server,
		ServerPort:   proxy.Port,
		Extra:        extra,
		Country:      country,
		CountryEmoji: countryEmoji,
	}

	return node, nil
}

// parseBandwidthClash 解析带宽字符串为 Mbps 整数
// 支持格式: "100", "100Mbps", "100 mbps", "100M" 等
func parseBandwidthClash(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "mbps")
	s = strings.TrimSuffix(s, "m")
	s = strings.TrimSpace(s)
	if v, err := strconv.Atoi(s); err == nil && v > 0 {
		return v
	}
	return 0
}
