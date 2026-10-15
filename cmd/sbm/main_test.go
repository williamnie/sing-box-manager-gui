package main

import (
	"net"
	"strconv"
	"testing"
)

func TestListenSecurityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, host, cert, key, networks string
		port                            int
		allowHTTP                       bool
		ok                              bool
	}{
		{"loopback", "127.0.0.1", "", "", "", 9090, false, true},
		{"ipv6 loopback", "::1", "", "", "", 9090, false, true},
		{"public plaintext", "0.0.0.0", "", "", "", 9090, false, false},
		{"lan plaintext", "192.168.50.142", "", "", "", 9090, false, false},
		{"ipv6 public plaintext", "::", "", "", "", 9090, false, false},
		{"tls public", "0.0.0.0", "cert", "key", "192.168.50.0/24,::1/128", 9090, false, true},
		{"missing key", "127.0.0.1", "cert", "", "", 9090, false, false},
		{"hostname", "example.com", "cert", "key", "", 9090, false, false},
		{"bad cidr", "127.0.0.1", "", "", "all", 9090, false, false},
		{"bad port", "127.0.0.1", "", "", "", 0, false, false},
		{"opt-in loopback", "127.0.0.1", "", "", "", 9090, true, true},
		{"opt-in public plaintext", "0.0.0.0", "", "", "", 9090, true, true},
		{"opt-in lan plaintext", "192.168.50.142", "", "", "192.168.50.0/24,::1/128", 9090, true, true},
		{"opt-in ipv6 public plaintext", "::", "", "", "", 9090, true, true},
		{"opt-in ipv6 lan plaintext", "fd00::142", "", "", "", 9090, true, true},
		{"opt-in with tls", "0.0.0.0", "cert", "key", "", 9090, true, true},
		{"opt-in missing key", "0.0.0.0", "cert", "", "", 9090, true, false},
		{"opt-in missing cert", "0.0.0.0", "", "key", "", 9090, true, false},
		{"opt-in hostname", "example.com", "", "", "", 9090, true, false},
		{"opt-in empty host", "", "", "", "", 9090, true, false},
		{"opt-in bad cidr", "0.0.0.0", "", "", "all", 9090, true, false},
		{"opt-in bad port", "0.0.0.0", "", "", "", 0, true, false},
		{"opt-in port overflow", "0.0.0.0", "", "", "", 65536, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, allowed, err := validateListen(tc.host, tc.port, tc.cert, tc.key, tc.networks, tc.allowHTTP)
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected validation: %v", err)
			}
			if !tc.ok {
				return
			}
			if want := net.JoinHostPort(tc.host, strconv.Itoa(tc.port)); addr != want {
				t.Fatalf("listen address = %q, want %q", addr, want)
			}
			if tc.networks == "" {
				if len(allowed) != 0 {
					t.Fatalf("unexpected source restrictions: %v", allowed)
				}
			} else if len(allowed) != 2 || allowed[0].String() != "192.168.50.0/24" || allowed[1].String() != "::1/128" {
				t.Fatalf("source restrictions not preserved: %v", allowed)
			}
		})
	}
}
