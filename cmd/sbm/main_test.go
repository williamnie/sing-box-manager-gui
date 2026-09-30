package main

import "testing"

func TestListenSecurityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, host, cert, key, networks string
		port                            int
		ok                              bool
	}{
		{"loopback", "127.0.0.1", "", "", "", 9090, true},
		{"ipv6 loopback", "::1", "", "", "", 9090, true},
		{"public plaintext", "0.0.0.0", "", "", "", 9090, false},
		{"lan plaintext", "192.168.50.142", "", "", "", 9090, false},
		{"ipv6 public plaintext", "::", "", "", "", 9090, false},
		{"tls public", "0.0.0.0", "cert", "key", "192.168.50.0/24,::1/128", 9090, true},
		{"missing key", "127.0.0.1", "cert", "", "", 9090, false},
		{"hostname", "example.com", "cert", "key", "", 9090, false},
		{"bad cidr", "127.0.0.1", "", "", "all", 9090, false},
		{"bad port", "127.0.0.1", "", "", "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := validateListen(tc.host, tc.port, tc.cert, tc.key, tc.networks)
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
