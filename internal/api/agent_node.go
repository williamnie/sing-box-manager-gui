package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// builder 会把 Extra 原样并入出站，sing-box check 只判断配置合法，不判断是否越界；
// 因此 Agent 写入的节点（手动、订阅）按协议与字段白名单校验，禁止本地文件引用与保留键覆盖。
var agentNodeTypes = map[string]bool{"shadowsocks": true, "vmess": true, "vless": true, "trojan": true, "hysteria": true, "hysteria2": true, "tuic": true, "socks": true, "http": true}

var agentNodeTopFields = map[string]bool{
	"method": true, "password": true, "plugin": true, "plugin_opts": true, "network": true, "udp_over_tcp": true, "multiplex": true,
	"uuid": true, "security": true, "alter_id": true, "global_padding": true, "authenticated_length": true, "tls": true,
	"packet_encoding": true, "transport": true, "flow": true, "up_mbps": true, "down_mbps": true, "obfs": true, "auth_str": true,
	"server_ports": true, "hop_interval": true, "congestion_control": true, "udp_relay_mode": true, "udp_over_stream": true,
	"zero_rtt_handshake": true, "heartbeat": true, "version": true, "username": true, "headers": true, "connect_timeout": true, "tcp_fast_open": true,
}

// 只有 http 出站的 path 是协议字段；其余类型的 path 只能出现在 transport 中。
var agentNodeTypeOnly = map[string]string{"path": "http"}

var agentNodeNested = map[string]map[string]bool{
	"tls":       {"enabled": true, "disable_sni": true, "server_name": true, "insecure": true, "alpn": true, "min_version": true, "max_version": true, "cipher_suites": true, "certificate": true, "utls": true, "reality": true, "ech": true, "fragment": true, "record_fragment": true},
	"utls":      {"enabled": true, "fingerprint": true},
	"reality":   {"enabled": true, "public_key": true, "short_id": true},
	"ech":       {"enabled": true, "config": true},
	"transport": {"type": true, "host": true, "path": true, "headers": true, "method": true, "service_name": true, "idle_timeout": true, "ping_timeout": true, "permit_without_stream": true, "max_early_data": true, "early_data_header_name": true},
	"multiplex": {"enabled": true, "protocol": true, "max_connections": true, "min_streams": true, "max_streams": true, "padding": true, "brutal": true},
	"brutal":    {"enabled": true, "up_mbps": true, "down_mbps": true},
	"obfs":      {"type": true, "password": true},
}

var agentPlugins = map[string]bool{"": true, "obfs-local": true, "v2ray-plugin": true}

func validateAgentNodeValue(path, key string, value any) error {
	if strings.HasSuffix(key, "_path") || strings.HasSuffix(key, "_file") {
		return fmt.Errorf("%s 不允许引用本地文件", path)
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	allowed, known := agentNodeNested[key]
	// headers 是任意请求头名称，值只能是字符串或字符串列表。
	if key == "headers" {
		for name, v := range m {
			switch x := v.(type) {
			case string:
			case []any:
				for _, item := range x {
					if _, ok := item.(string); !ok {
						return fmt.Errorf("%s.%s 只能是文本", path, name)
					}
				}
			default:
				return fmt.Errorf("%s.%s 只能是文本", path, name)
			}
		}
		return nil
	}
	if !known {
		return fmt.Errorf("%s 不是允许的对象字段", path)
	}
	for k, v := range m {
		if strings.HasSuffix(k, "_path") || strings.HasSuffix(k, "_file") {
			return fmt.Errorf("%s.%s 不允许引用本地文件", path, k)
		}
		if !allowed[k] {
			return fmt.Errorf("%s.%s 不在允许的字段中", path, k)
		}
		if err := validateAgentNodeValue(path+"."+k, k, v); err != nil {
			return err
		}
	}
	return nil
}

// validateAgentNode 用于 Agent 新建/修改的手动节点与 Agent 拉取的订阅节点。
func validateAgentNode(node storage.Node) error {
	if !agentNodeTypes[node.Type] {
		return fmt.Errorf("节点 %s 的协议 %q 不在允许范围", node.Tag, node.Type)
	}
	keys := make([]string, 0, len(node.Extra))
	for k := range node.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if only, ok := agentNodeTypeOnly[k]; ok {
			if node.Type != only {
				return fmt.Errorf("节点 %s 的字段 %s 只允许用于 %s", node.Tag, k, only)
			}
		} else if !agentNodeTopFields[k] {
			return fmt.Errorf("节点 %s 的字段 %s 不允许（含 type/tag/server 等保留键）", node.Tag, k)
		}
		if k == "plugin" {
			if p, _ := node.Extra[k].(string); !agentPlugins[p] {
				return fmt.Errorf("节点 %s 的插件 %q 不允许", node.Tag, p)
			}
		}
		if err := validateAgentNodeValue(k, k, node.Extra[k]); err != nil {
			return fmt.Errorf("节点 %s：%s", node.Tag, err.Error())
		}
	}
	return nil
}

func validateAgentNodes(nodes []storage.Node) error {
	for _, n := range nodes {
		if err := validateAgentNode(n); err != nil {
			return err
		}
	}
	return nil
}
