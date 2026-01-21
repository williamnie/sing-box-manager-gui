package parser

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/storage"
	"github.com/xiaobei/singbox-manager/pkg/utils"
)

// Parser 解析器接口
type Parser interface {
	Parse(rawURL string) (*storage.Node, error)
	Protocol() string
}

// ParseURL 解析代理 URL
func ParseURL(rawURL string) (*storage.Node, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("URL 为空")
	}

	// 获取协议类型
	idx := strings.Index(rawURL, "://")
	if idx == -1 {
		return nil, fmt.Errorf("无效的 URL 格式")
	}
	protocol := strings.ToLower(rawURL[:idx])

	var parser Parser
	switch protocol {
	case "ss":
		parser = &ShadowsocksParser{}
	case "vmess":
		parser = &VmessParser{}
	case "vless":
		parser = &VlessParser{}
	case "trojan":
		parser = &TrojanParser{}
	case "hysteria2", "hy2", "hysteria":
		parser = &Hysteria2Parser{}
	case "tuic":
		parser = &TuicParser{}
	case "socks", "socks5", "socks4", "socks4a":
		parser = &SocksParser{}
	default:
		return nil, fmt.Errorf("不支持的协议: %s", protocol)
	}

	node, err := parser.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	// 解析国家信息
	if country := utils.ParseCountryFromNodeName(node.Tag); country != nil {
		node.Country = utils.InternCountry(country.Code)
		node.CountryEmoji = utils.InternEmoji(country.Emoji)
	}

	return node, nil
}

// ParseSubscriptionContent 解析订阅内容
func ParseSubscriptionContent(content string) ([]storage.Node, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("订阅内容为空")
	}

	var nodes []storage.Node

	// 尝试作为 Clash YAML 解析
	if strings.Contains(content, "proxies:") {
		yamlNodes, err := ParseClashYAML(content)
		if err == nil && len(yamlNodes) > 0 {
			return yamlNodes, nil
		}
	}

	// 尝试 Base64 解码
	if utils.IsBase64(content) && !strings.Contains(content, "://") {
		decoded, err := utils.DecodeBase64(content)
		if err == nil {
			content = decoded
		}
	}

	// 按行解析
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 尝试 Base64 解码单行
		if utils.IsBase64(line) && !strings.Contains(line, "://") {
			if decoded, err := utils.DecodeBase64(line); err == nil {
				line = decoded
			}
		}

		// 如果解码后包含多行，递归解析
		if strings.Contains(line, "\n") {
			subNodes, err := ParseSubscriptionContent(line)
			if err == nil {
				nodes = append(nodes, subNodes...)
			}
			continue
		}

		// 解析单个 URL
		if strings.Contains(line, "://") {
			node, err := ParseURL(line)
			if err == nil && node != nil {
				nodes = append(nodes, *node)
			}
		}
	}

	return nodes, nil
}

// parseServerInfo 解析服务器地址和端口
func parseServerInfo(serverInfo string) (host string, port int, err error) {
	serverInfo = strings.TrimSpace(serverInfo)

	// 处理 IPv6 地址 [::1]:8080
	if strings.HasPrefix(serverInfo, "[") {
		idx := strings.LastIndex(serverInfo, "]:")
		if idx == -1 {
			return "", 0, fmt.Errorf("无效的服务器地址: %s", serverInfo)
		}
		host = serverInfo[1:idx]
		portStr := serverInfo[idx+2:]
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("无效的端口: %s", portStr)
		}
		return host, port, nil
	}

	// 处理普通地址 host:port
	parts := strings.Split(serverInfo, ":")
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("无效的服务器地址: %s", serverInfo)
	}

	// 最后一个是端口
	portStr := parts[len(parts)-1]
	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("无效的端口: %s", portStr)
	}

	// 其余是主机名
	host = strings.Join(parts[:len(parts)-1], ":")

	return host, port, nil
}

// parseURLParams 解析 URL 参数
func parseURLParams(rawURL string) (addressPart string, params url.Values, name string, err error) {
	// 分离协议
	idx := strings.Index(rawURL, "://")
	if idx == -1 {
		return "", nil, "", fmt.Errorf("无效的 URL")
	}
	rest := rawURL[idx+3:]

	// 分离 fragment (#name)
	if fragIdx := strings.Index(rest, "#"); fragIdx != -1 {
		name, _ = url.QueryUnescape(rest[fragIdx+1:])
		rest = rest[:fragIdx]
	}

	// 分离查询参数
	if queryIdx := strings.Index(rest, "?"); queryIdx != -1 {
		queryStr := rest[queryIdx+1:]
		params, _ = url.ParseQuery(queryStr)
		addressPart = rest[:queryIdx]
	} else {
		addressPart = rest
		params = url.Values{}
	}

	return addressPart, params, name, nil
}

// getParamString 获取字符串参数
func getParamString(params url.Values, key string, defaultValue string) string {
	if v := params.Get(key); v != "" {
		return v
	}
	return defaultValue
}

// getParamBool 获取布尔参数
func getParamBool(params url.Values, key string) bool {
	v := params.Get(key)
	return v == "1" || v == "true" || v == "True" || v == "TRUE"
}

// getParamInt 获取整数参数
func getParamInt(params url.Values, key string, defaultValue int) int {
	if v := params.Get(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultValue
}
