package parser

import (
	"testing"
)

func TestParseClashYAML_TCPWithUnusedTransportOptions(t *testing.T) {
	for _, network := range []string{"", "    network: tcp\n"} {
		t.Run(network, func(t *testing.T) {
			content := "proxies:\n  - name: tcp-node\n    type: vless\n    server: example.com\n    port: 443\n    uuid: 11111111-1111-1111-1111-111111111111\n" + network + "    ws-opts: {}\n    h2-opts: {}\n    grpc-opts: {}\n"
			nodes, err := ParseClashYAML(content)
			if err != nil || len(nodes) != 1 {
				t.Fatalf("parse: nodes=%d err=%v", len(nodes), err)
			}
			if transport, exists := nodes[0].Extra["transport"]; exists {
				t.Fatalf("TCP unexpectedly generated transport: %#v", transport)
			}
		})
	}
}

func TestParseClashYAML_WebSocketKeepsTransportOptions(t *testing.T) {
	nodes, err := ParseClashYAML(`proxies:
  - name: ws-node
    type: vless
    server: example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    network: ws
    ws-opts:
      path: /relay
`)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("parse: nodes=%d err=%v", len(nodes), err)
	}
	transport, ok := nodes[0].Extra["transport"].(map[string]interface{})
	if !ok || transport["type"] != "ws" || transport["path"] != "/relay" {
		t.Fatalf("WebSocket options not preserved: %#v", transport)
	}
}

func TestParseClashYAML_TUICIncludesTLS(t *testing.T) {
	content := `
proxies:
  - name: "TUIC Test"
    type: tuic
    server: example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    password: secret
    sni: edge.example.com
    skip-cert-verify: true
    alpn:
      - h3
    congestion-controller: bbr
    udp-relay-mode: native
    reduce-rtt: true
`

	nodes, err := ParseClashYAML(content)
	if err != nil {
		t.Fatalf("ParseClashYAML() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("ParseClashYAML() node count = %d, want 1", len(nodes))
	}

	node := nodes[0]
	if node.Type != "tuic" {
		t.Fatalf("node.Type = %q, want %q", node.Type, "tuic")
	}

	tls, ok := node.Extra["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("node.Extra[\"tls\"] type = %T, want map[string]interface{}", node.Extra["tls"])
	}
	if enabled, ok := tls["enabled"].(bool); !ok || !enabled {
		t.Fatalf("tls.enabled = %#v, want true", tls["enabled"])
	}
	if serverName := tls["server_name"]; serverName != "edge.example.com" {
		t.Fatalf("tls.server_name = %#v, want %q", serverName, "edge.example.com")
	}
	if insecure, ok := tls["insecure"].(bool); !ok || !insecure {
		t.Fatalf("tls.insecure = %#v, want true", tls["insecure"])
	}

	alpn, ok := tls["alpn"].([]string)
	if !ok || len(alpn) != 1 || alpn[0] != "h3" {
		t.Fatalf("tls.alpn = %#v, want [\"h3\"]", tls["alpn"])
	}
}
