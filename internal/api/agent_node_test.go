package api

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/parser"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 白名单不能误伤分享链接解析出的正常节点。
func TestAgentNodeAllowlistAcceptsParsedLinks(t *testing.T) {
	vmess := base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"198.51.100.1","port":"443","id":"0b9c8f4e-2b2f-4f55-9a43-1b2c3d4e5f60","aid":"0","net":"ws","type":"none","host":"cdn.example","path":"/ws","tls":"tls","sni":"cdn.example"}`))
	links := []string{
		"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-128-gcm:pass")) + "@198.51.100.2:8388#ss",
		"vmess://" + vmess,
		"vless://0b9c8f4e-2b2f-4f55-9a43-1b2c3d4e5f60@198.51.100.3:443?encryption=none&security=reality&sni=www.example.com&fp=chrome&pbk=AbCdEf0123456789AbCdEf0123456789AbCdEf01234&sid=ab12&type=grpc&serviceName=grpcsvc&flow=xtls-rprx-vision#vless",
		"trojan://secret@198.51.100.4:443?sni=t.example&type=ws&path=%2Fp&host=t.example#trojan",
		"hysteria2://secret@198.51.100.5:443?sni=h.example&obfs=salamander&obfs-password=o&insecure=1#hy2",
		"tuic://0b9c8f4e-2b2f-4f55-9a43-1b2c3d4e5f60:pass@198.51.100.6:443?congestion_control=bbr&alpn=h3&sni=u.example#tuic",
		"socks5://user:pass@198.51.100.7:1080#socks",
	}
	for _, link := range links {
		node, err := parser.ParseURL(link)
		if err != nil {
			t.Fatalf("parse %s: %v", link[:12], err)
		}
		if err := validateAgentNode(*node); err != nil {
			t.Fatalf("%s rejected: %v (extra %v)", node.Type, err, node.Extra)
		}
	}
}

func TestAgentNodeAllowlistRejectsEscapes(t *testing.T) {
	base := func(extra map[string]any) storage.Node {
		return storage.Node{Tag: "n", Type: "trojan", Server: "198.51.100.1", ServerPort: 443, Extra: extra}
	}
	bad := map[string]storage.Node{
		"tls client key path":   base(map[string]any{"password": "x", "tls": map[string]any{"enabled": true, "client_key_path": "/etc/shadow"}}),
		"tls certificate path":  base(map[string]any{"password": "x", "tls": map[string]any{"enabled": true, "certificate_path": "/root/ca.pem"}}),
		"ech config path":       base(map[string]any{"tls": map[string]any{"ech": map[string]any{"enabled": true, "config_path": "/x"}}}),
		"reserved server key":   base(map[string]any{"server": "127.0.0.1"}),
		"reserved type key":     base(map[string]any{"type": "direct"}),
		"dial bind interface":   base(map[string]any{"bind_interface": "eth1"}),
		"detour":                base(map[string]any{"detour": "DIRECT"}),
		"unknown tls field":     base(map[string]any{"tls": map[string]any{"store": "x"}}),
		"non-http path":         base(map[string]any{"path": "/x"}),
		"unknown protocol":      {Tag: "n", Type: "direct", Server: "x", ServerPort: 1},
		"external plugin":       {Tag: "n", Type: "shadowsocks", Server: "x", ServerPort: 1, Extra: map[string]any{"method": "aes-128-gcm", "password": "p", "plugin": "/usr/bin/evil"}},
		"header non-text value": base(map[string]any{"transport": map[string]any{"type": "ws", "headers": map[string]any{"Host": map[string]any{"x": 1}}}}),
	}
	for name, node := range bad {
		if err := validateAgentNode(node); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	ok := base(map[string]any{"password": "x", "transport": map[string]any{"type": "ws", "path": "/ws", "headers": map[string]any{"Host": "cdn.example"}}, "tls": map[string]any{"enabled": true, "server_name": "a", "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}}})
	if err := validateAgentNode(ok); err != nil {
		t.Fatal("ws transport path rejected:", err)
	}
}

// 通过 Agent API 提交带本地文件引用的节点与订阅都会被拒绝。
func TestAgentRejectsUnsafeNodesThroughAPI(t *testing.T) {
	f := newAgentFixture(t)
	node := map[string]any{"ops": []any{map[string]any{"action": "create", "resource": "manual_node", "value": map[string]any{"node": map[string]any{
		"tag": "evil", "type": "trojan", "server": "198.51.100.1", "server_port": 443,
		"extra": map[string]any{"password": "x", "tls": map[string]any{"enabled": true, "client_key_path": "/etc/shadow"}},
	}}}}}
	if w := f.call("POST", "/api/agent/v1/changesets", node); w.Code != 400 || !strings.Contains(w.Body.String(), "本地文件") {
		t.Fatal(w.Code, w.Body.String())
	}
	f.s.fetchSubscription = func(sub *storage.Subscription) error {
		sub.Nodes = []storage.Node{{Tag: "bad", Type: "vmess", Server: "198.51.100.1", ServerPort: 443, Extra: map[string]any{"uuid": "u", "tls": map[string]any{"certificate_path": "/x"}}}}
		return nil
	}
	sub := map[string]any{"ops": []any{map[string]any{"action": "create", "resource": "subscription", "value": map[string]any{"name": "s", "url": "https://sub.example/x"}}}}
	if w := f.call("POST", "/api/agent/v1/changesets", sub); w.Code != 400 {
		t.Fatal("unsafe subscription accepted", w.Code, w.Body.String())
	}
}
