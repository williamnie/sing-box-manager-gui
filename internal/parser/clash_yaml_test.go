package parser

import (
	"reflect"
	"testing"
)

func TestParseServerPorts(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "normal ports and range",
			in:   "1000,2000-3000,5000",
			want: []string{"1000", "2000:3000", "5000"},
		},
		{
			name: "with spaces",
			in:   " 1000 , 2000-3000 , 5000 ",
			want: []string{"1000", "2000:3000", "5000"},
		},
		{
			name: "empty",
			in:   "",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseServerPorts(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseServerPorts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNormalizeHopInterval(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "int", in: 15, want: "15s"},
		{name: "float", in: 1.5, want: "1.5s"},
		{name: "numeric string", in: "30", want: "30s"},
		{name: "duration string", in: "2m", want: "2m"},
		{name: "empty string", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeHopInterval(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeHopInterval() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConvertClashProxy_Hysteria2PortsAndHopInterval(t *testing.T) {
	proxy := ClashProxy{
		Name:        "hy2-test",
		Type:        "hy2",
		Server:      "example.com",
		Port:        443,
		Password:    "secret",
		Ports:       "1000,2000-3000",
		HopInterval: 15,
	}

	node, err := convertClashProxy(proxy)
	if err != nil {
		t.Fatalf("convertClashProxy() error = %v", err)
	}

	serverPorts, ok := node.Extra["server_ports"].([]string)
	if !ok {
		t.Fatalf("server_ports type = %T, want []string", node.Extra["server_ports"])
	}
	if !reflect.DeepEqual(serverPorts, []string{"1000", "2000:3000"}) {
		t.Fatalf("server_ports = %v, want [1000 2000:3000]", serverPorts)
	}

	hopInterval, ok := node.Extra["hop_interval"].(string)
	if !ok || hopInterval != "15s" {
		t.Fatalf("hop_interval = %v, want %q", node.Extra["hop_interval"], "15s")
	}
}

func TestConvertClashProxyTLSFingerprintPriority(t *testing.T) {
	tests := []struct {
		name                string
		clientFingerprint   string
		fingerprint         string
		wantUTLSFingerprint string
	}{
		{
			name:                "prefer client-fingerprint",
			clientFingerprint:   "chrome",
			fingerprint:         "safari",
			wantUTLSFingerprint: "chrome",
		},
		{
			name:                "fallback to fingerprint",
			clientFingerprint:   "",
			fingerprint:         "firefox",
			wantUTLSFingerprint: "firefox",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := ClashProxy{
				Name:              "vmess-test",
				Type:              "vmess",
				Server:            "example.com",
				Port:              443,
				UUID:              "11111111-1111-1111-1111-111111111111",
				TLS:               true,
				ClientFingerprint: tt.clientFingerprint,
				Fingerprint:       tt.fingerprint,
			}

			node, err := convertClashProxy(proxy)
			if err != nil {
				t.Fatalf("convertClashProxy() error = %v", err)
			}

			tls, ok := node.Extra["tls"].(map[string]interface{})
			if !ok {
				t.Fatalf("tls type = %T, want map[string]interface{}", node.Extra["tls"])
			}

			utls, ok := tls["utls"].(map[string]interface{})
			if !ok {
				t.Fatalf("utls type = %T, want map[string]interface{}", tls["utls"])
			}

			fingerprint, ok := utls["fingerprint"].(string)
			if !ok || fingerprint != tt.wantUTLSFingerprint {
				t.Fatalf("utls.fingerprint = %v, want %q", utls["fingerprint"], tt.wantUTLSFingerprint)
			}
		})
	}
}

func TestConvertClashProxyTUICPasswordFallbackToToken(t *testing.T) {
	proxy := ClashProxy{
		Name:   "tuic-test",
		Type:   "tuic",
		Server: "example.com",
		Port:   443,
		UUID:   "11111111-1111-1111-1111-111111111111",
		Token:  "tuic-token",
	}

	node, err := convertClashProxy(proxy)
	if err != nil {
		t.Fatalf("convertClashProxy() error = %v", err)
	}

	password, ok := node.Extra["password"].(string)
	if !ok || password != "tuic-token" {
		t.Fatalf("password = %v, want %q", node.Extra["password"], "tuic-token")
	}
}
