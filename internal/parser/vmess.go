package parser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/storage"
	"github.com/xiaobei/singbox-manager/pkg/utils"
)

// VmessParser VMess 解析器
type VmessParser struct{}

// Protocol 返回协议名称
func (p *VmessParser) Protocol() string {
	return "vmess"
}

// vmessConfig VMess 配置结构
type vmessConfig struct {
	V    interface{} `json:"v"`                // 版本
	Ps   string      `json:"ps"`               // 节点名称
	Add  string      `json:"add"`              // 服务器地址
	Port interface{} `json:"port"`             // 端口
	ID   string      `json:"id"`               // UUID
	Aid  interface{} `json:"aid"`              // Alter ID
	Scy  string      `json:"scy"`              // 加密方式
	Net  string      `json:"net"`              // 传输协议
	Type string      `json:"type"`             // 伪装类型
	Host string      `json:"host"`             // 伪装域名
	Path string      `json:"path"`             // 路径
	TLS  string      `json:"tls"`              // TLS
	SNI  string      `json:"sni"`              // SNI
	ALPN string      `json:"alpn"`             // ALPN
	Fp   string      `json:"fp"`               // Fingerprint
	Skip bool        `json:"skip-cert-verify"` // 跳过证书验证
}

// Parse 解析 VMess URL
// 格式: vmess://BASE64(json)#name
func (p *VmessParser) Parse(rawURL string) (*storage.Node, error) {
	// 去除协议头
	rawURL = strings.TrimPrefix(rawURL, "vmess://")

	// 分离 fragment (#name)
	var fragmentName string
	if idx := strings.Index(rawURL, "#"); idx != -1 {
		fragmentName, _ = url.QueryUnescape(rawURL[idx+1:])
		rawURL = rawURL[:idx]
	}

	// Base64 解码
	decoded, err := utils.DecodeBase64(rawURL)
	if err != nil {
		return nil, fmt.Errorf("Base64 解码失败: %w", err)
	}

	// 解析 JSON
	var config vmessConfig
	if err := json.Unmarshal([]byte(decoded), &config); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}

	// 获取端口
	var port int
	switch v := config.Port.(type) {
	case float64:
		port = int(v)
	case string:
		port, _ = strconv.Atoi(v)
	case int:
		port = v
	}

	// 获取 Alter ID
	var alterId int
	switch v := config.Aid.(type) {
	case float64:
		alterId = int(v)
	case string:
		alterId, _ = strconv.Atoi(v)
	case int:
		alterId = v
	}

	// 设置名称
	name := config.Ps
	if fragmentName != "" {
		name = fragmentName
	}
	if name == "" {
		name = fmt.Sprintf("%s:%d", config.Add, port)
	}

	// 构建 Extra
	extra := map[string]interface{}{
		"uuid":     config.ID,
		"alter_id": alterId,
		"security": config.Scy,
	}

	// 设置默认加密方式
	if config.Scy == "" {
		extra["security"] = "auto"
	}

	// 传输层配置
	network := config.Net
	if network == "" {
		network = "tcp"
	}

	// 构建传输配置
	if network != "tcp" || config.Type == "http" {
		transport := map[string]interface{}{
			"type": network,
		}

		switch network {
		case "ws":
			if config.Path != "" {
				transport["path"] = config.Path
			}
			if config.Host != "" {
				transport["headers"] = map[string]string{
					"Host": config.Host,
				}
			}
		case "http", "h2":
			if config.Path != "" {
				transport["path"] = config.Path
			}
			if config.Host != "" {
				transport["host"] = strings.Split(config.Host, ",")
			}
		case "grpc":
			if config.Path != "" {
				transport["service_name"] = config.Path
			}
		case "quic":
			if config.Type != "" {
				transport["security"] = config.Type
			}
		}

		extra["transport"] = transport
	}

	// TLS 配置
	if config.TLS == "tls" {
		tls := map[string]interface{}{
			"enabled": true,
		}
		// 设置 server_name（按优先级：SNI > Host > 服务器地址）
		if config.SNI != "" {
			tls["server_name"] = config.SNI
		} else if config.Host != "" {
			tls["server_name"] = config.Host
		} else {
			// 如果 SNI 和 Host 都为空，使用服务器地址作为默认 server_name
			// 这是为了确保 TLS 握手时有正确的 SNI
			tls["server_name"] = config.Add
		}
		if config.Skip {
			tls["insecure"] = true
		}
		if config.Fp != "" {
			tls["utls"] = map[string]interface{}{
				"enabled":     true,
				"fingerprint": config.Fp,
			}
		}
		if config.ALPN != "" {
			tls["alpn"] = strings.Split(config.ALPN, ",")
		}
		extra["tls"] = tls
	}

	node := &storage.Node{
		Tag:        name,
		Type:       "vmess",
		Server:     config.Add,
		ServerPort: port,
		Extra:      extra,
	}

	return node, nil
}
