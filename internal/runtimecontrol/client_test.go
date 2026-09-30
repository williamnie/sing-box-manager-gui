package runtimecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewClient(strings.TrimPrefix(server.URL, "http://"), "test-private-secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.http.CloseIdleConnections)
	return client
}

func TestControllerAllowsOnlyLiteralLoopback(t *testing.T) {
	for _, controller := range []string{"127.0.0.1:9090", "127.0.0.2:9090", "[::1]:9090", "0.0.0.0:9090", "[::]:9090", ":9090"} {
		client, err := NewClient(controller, "")
		if err != nil {
			t.Errorf("NewClient(%q): %v", controller, err)
			continue
		}
		host, _, err := net.SplitHostPort(client.base.Host)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			t.Errorf("controller %q mapped to non-loopback %q", controller, client.base.Host)
		}
	}
	for _, controller := range []string{"", "localhost:9090", "example.com:9090", "192.168.1.1:9090", "8.8.8.8:9090", "http://127.0.0.1:9090", "user:password@127.0.0.1:9090", "127.0.0.1:9090/path", "127.0.0.1:0", "127.0.0.1:65536", "[::1%lo0]:9090", "127.0.0.1:9090?secret=foo"} {
		if _, err := NewClient(controller, ""); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("NewClient(%q) should reject: %v", controller, err)
		}
	}
}

func TestClientUsesBearerAndIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:1")
	t.Setenv("HTTPS_PROXY", "http://192.0.2.1:1")
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" || r.Header.Get("Authorization") != "Bearer test-private-secret" {
			t.Errorf("unexpected version request path or authorization")
		}
		fmt.Fprint(w, `{"version":"sing-box 1.13.0","extra":"ignored"}`)
	})
	transport := client.http.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("controller requests must not use an HTTP proxy")
	}
	version, err := client.Version(context.Background())
	if err != nil || version != "sing-box 1.13.0" {
		t.Fatalf("Version() = %q, %v", version, err)
	}
}

func TestProxiesDecodeGroupsAndUnknownDelay(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"proxies":{"节点/甲%25":{"name":"ignored","type":"Shadowsocks","history":[{"delay":100},{"delay":0}],"secret":"must not leak"},"手动":{"type":"sElEcToR","all":["节点/甲%25","DIRECT"],"now":"节点/甲%25","history":[{"delay":23}]},"自动":{"type":"URLTest","all":["节点/甲%25"],"now":"节点/甲%25","history":[]},"DIRECT":{"type":"Direct"}}}`)
	})
	proxies, err := client.Proxies(context.Background())
	if err != nil || len(proxies) != 4 {
		t.Fatalf("Proxies() = %#v, %v", proxies, err)
	}
	byTag := map[string]Proxy{}
	for _, proxy := range proxies {
		byTag[proxy.Tag] = proxy
		if proxy.Members == nil {
			t.Errorf("members must be [] instead of null: %q", proxy.Tag)
		}
	}
	manual := byTag["手动"]
	if !manual.Selectable || manual.Selected != "节点/甲%25" || manual.Delay == nil || *manual.Delay != 23 {
		t.Errorf("selector = %#v", manual)
	}
	if byTag["自动"].Selectable || byTag["自动"].Delay != nil || byTag["节点/甲%25"].Delay != nil {
		t.Error("URLTest must not be selectable and absent/failed delays must remain null")
	}
	encoded, _ := json.Marshal(proxies)
	if strings.Contains(string(encoded), "must not leak") || strings.Contains(string(encoded), "history") {
		t.Fatal("upstream fields escaped the normalized DTO")
	}
}

func TestSelectEscapesTagsAndVerifiesCurrentSelection(t *testing.T) {
	group := "代理 /?%25#组"
	member := "香港/50% 节点"
	selected := "DIRECT"
	puts := 0
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{group: map[string]any{"type": "Selector", "all": []string{"DIRECT", member}, "now": selected}}})
		case http.MethodPut:
			puts++
			if r.URL.EscapedPath() != "/proxies/%E4%BB%A3%E7%90%86%20%2F%3F%2525%23%E7%BB%84" {
				t.Errorf("path did not preserve tag as one escaped segment: %s", r.URL.EscapedPath())
			}
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != member {
				t.Errorf("PUT member = %q, %v", body.Name, err)
			}
			selected = body.Name
			w.WriteHeader(http.StatusNoContent)
		}
	})
	if err := client.Select(context.Background(), group, member); err != nil {
		t.Fatal(err)
	}
	if puts != 1 {
		t.Fatalf("PUT count = %d", puts)
	}
}

func TestSelectRejectsNonSelectorsAndUnknownMembers(t *testing.T) {
	var puts atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
		}
		fmt.Fprint(w, `{"proxies":{"手动":{"type":"Selector","all":["a"],"now":"a"},"自动":{"type":"URLTest","all":["a"],"now":"a"}}}`)
	})
	for _, input := range [][2]string{{"手动", "not-member"}, {"自动", "a"}, {"missing", "a"}, {"", "a"}} {
		if err := client.Select(context.Background(), input[0], input[1]); err == nil {
			t.Errorf("Select(%q, %q) should fail", input[0], input[1])
		}
	}
	if puts.Load() != 0 {
		t.Fatal("invalid selections reached upstream mutations")
	}
}

func TestSelectDoesNotClaimUnconfirmedSuccess(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprint(w, `{"proxies":{"group":{"type":"Selector","all":["a","b"],"now":"a"}}}`)
	})
	if err := client.Select(context.Background(), "group", "b"); !errors.Is(err, ErrSelectionChanged) {
		t.Fatalf("unconfirmed selection: %v", err)
	}
}

func TestDelayValidatesDestinationAndTimeout(t *testing.T) {
	var requests atomic.Int32
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.EscapedPath() != "/proxies/%E8%8A%82%E7%82%B9%2F%25/delay" || r.URL.Query().Get("url") != "https://1.1.1.1/generate_204" || r.URL.Query().Get("timeout") != "1000" {
			t.Errorf("unexpected delay request %s", r.URL.String())
		}
		fmt.Fprint(w, `{"delay":42}`)
	})
	for _, testURL := range []string{"http://example.com", "https://user:secret@example.com", "https://127.0.0.1", "https://10.1.2.3", "https://169.254.169.254", "https://[::1]", "https://[fd00::1]", "https://[::ffff:127.0.0.1]", "https://100.64.0.1", "https://localhost", "file:///tmp/example", "https://1.1.1.1/#fragment", "https://1.1.1.1:70000"} {
		if _, err := client.Delay(context.Background(), "节点/%", testURL, 1000); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Delay(%q) should reject: %v", testURL, err)
		}
	}
	for _, timeout := range []int{-1, 0, 99, 10001} {
		if _, err := client.Delay(context.Background(), "节点/%", "https://1.1.1.1/generate_204", timeout); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Delay(timeout=%d) should reject: %v", timeout, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("unsafe delay request reached upstream")
	}
	delay, err := client.Delay(context.Background(), "节点/%", "https://1.1.1.1/generate_204", 1000)
	if err != nil || delay == nil || *delay != 42 {
		t.Fatalf("Delay() = %v, %v", delay, err)
	}
}

func TestDelayRejectsPrivateDNSAnswers(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unsafe DNS target reached kernel") })
	client.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("192.168.1.1")}, nil
	}
	if _, err := client.Delay(context.Background(), "node", "https://example.com/test", 1000); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("mixed public/private DNS answers should fail: %v", err)
	}
}

func TestDelayNullRemainsUnknown(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"delay":null}`) })
	delay, err := client.Delay(context.Background(), "node", "https://1.1.1.1/test", 1000)
	if err != nil || delay != nil {
		t.Fatalf("null delay = %v, %v", delay, err)
	}
}

func TestDelayRejectsMissingResponseField(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
	if _, err := client.Delay(context.Background(), "node", "https://1.1.1.1/test", 1000); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("missing delay field must not look like a successful test: %v", err)
	}
}

func TestDelayAllowsValidatedPublicDomain(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != "https://example.com/test" {
			t.Error("validated hostname was replaced instead of preserving TLS hostname")
		}
		fmt.Fprint(w, `{"delay":12}`)
	})
	client.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2606:4700:4700::1111")}, nil
	}
	delay, err := client.Delay(context.Background(), "node", "https://example.com/test", 1000)
	if err != nil || delay == nil || *delay != 12 {
		t.Fatalf("public DNS target: %v, %v", delay, err)
	}
}

func TestDelayAllowsDomainResolvedToConfiguredFakeIP(t *testing.T) {
	for _, addresses := range [][]string{{"198.18.0.20"}, {"198.19.255.20"}, {"fc00::20"}, {"fc00:3fff::20"}, {"198.18.0.20", "fc00::20", "1.1.1.1"}} {
		t.Run(strings.Join(addresses, ","), func(t *testing.T) {
			var requested atomic.Bool
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				requested.Store(true)
				if r.URL.Query().Get("url") != "https://gstatic.test/generate_204" {
					t.Error("FakeIP must not replace the original HTTPS hostname")
				}
				fmt.Fprint(w, `{"delay":18}`)
			})
			client.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
				result := make([]net.IP, 0, len(addresses))
				for _, address := range addresses {
					result = append(result, net.ParseIP(address))
				}
				return result, nil
			}
			delay, err := client.Delay(context.Background(), "node", "https://gstatic.test/generate_204", 1000)
			if err != nil || delay == nil || *delay != 18 || !requested.Load() {
				t.Fatalf("configured FakeIP DNS answer must allow testing original hostname: delay=%v, err=%v", delay, err)
			}
		})
	}
}

func TestDelayStillRejectsLiteralFakeIPAndPrivateDestinations(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("private destination reached kernel") })
	for _, testURL := range []string{"https://198.18.0.20/test", "https://198.19.255.20/test", "https://[fc00::20]/test", "https://[fc00:3fff::20]/test"} {
		if _, err := client.Delay(context.Background(), "node", testURL, 1000); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("literal FakeIP %q should stay blocked: %v", testURL, err)
		}
	}
	for _, address := range []string{"10.1.2.3", "172.16.0.20", "192.168.1.20", "fc00:4000::20", "fd00::20", "127.0.0.1", "169.254.1.2"} {
		client.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("198.18.0.20"), net.ParseIP(address)}, nil
		}
		if _, err := client.Delay(context.Background(), "node", "https://gstatic.test/generate_204", 1000); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("private DNS answer %q should stay blocked: %v", address, err)
		}
	}
}

func TestDelayRejectsClearlyLocalHostnamesBeforeDNS(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("local hostname reached kernel") })
	client.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		t.Error("local hostname must be rejected before DNS/FakeIP resolution")
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	}
	for _, host := range []string{"localhost", "test.localhost", "router.local", "router.lan", "router.home", "metadata.internal", "router.home.arpa", "router", "Router.LAN."} {
		if _, err := client.Delay(context.Background(), "node", "https://"+host+"/test", 1000); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("obvious local hostname %q should reject: %v", host, err)
		}
	}
}

func TestDelayReportsAddressValidationFailureSeparately(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unresolved target reached kernel") })
	client.lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return nil, errors.New("resolver secret details must not escape")
	}
	_, err := client.Delay(context.Background(), "node", "https://gstatic.test/generate_204", 1000)
	if !errors.Is(err, ErrInvalidTestURL) || !errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("DNS validation failure must be attributable to test address: %v", err)
	}
	if !strings.Contains(err.Error(), "无法确认测速地址") || strings.Contains(err.Error(), "resolver secret") {
		t.Fatalf("address validation error lost attribution or exposed resolver details: %v", err)
	}
}

func TestProxyTagsWithDotSegmentsAreEscaped(t *testing.T) {
	for _, tag := range []string{".", "..", "a/../b", "香港%2F/节点"} {
		t.Run(tag, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.EscapedPath() != "/proxies/"+escapeTag(tag)+"/delay" || strings.Contains(r.URL.EscapedPath(), "/../") {
					t.Errorf("tag changed path structure: %s", r.URL.EscapedPath())
				}
				fmt.Fprint(w, `{"delay":12}`)
			})
			if _, err := client.Delay(context.Background(), tag, "https://1.1.1.1/test", 1000); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConnectionsDecodeSingBoxMetadataAndMissingFields(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"uploadTotal":1234,"downloadTotal":5678,"connections":[{"id":"11111111-1111-4111-8111-111111111111","metadata":{"network":"tcp","type":"mixed/mixed-in","sourceIP":"192.168.1.2","destinationIP":"1.1.1.1","sourcePort":"52345","destinationPort":443,"host":"example.com","processPath":"/Applications/test","secret":"hidden"},"upload":10,"download":20,"start":"2026-09-30T09:10:11Z","chains":["节点","Proxy"],"rule":"final"},{"id":"22222222-2222-4222-8222-222222222222"}]}`)
	})
	result, err := client.Connections(context.Background())
	if err != nil || result.UploadTotal != 1234 || result.DownloadTotal != 5678 || len(result.Connections) != 2 {
		t.Fatalf("Connections() = %#v, %v", result, err)
	}
	connection := result.Connections[0]
	if connection.Source != "192.168.1.2" || connection.SourcePort != "52345" || connection.DestinationPort != "443" || connection.Protocol != "mixed/mixed-in" || connection.Process != "/Applications/test" || connection.StartedAt != "2026-09-30T09:10:11Z" || len(connection.Chains) != 2 {
		t.Errorf("decoded connection = %#v", connection)
	}
	missing := result.Connections[1]
	if missing.Source != "" || missing.SourcePort != "" || missing.Process != "" || missing.Chains == nil {
		t.Errorf("missing fields were invented: %#v", missing)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "hidden") || strings.Contains(string(encoded), "metadata") {
		t.Fatal("connection DTO contains unapproved fields")
	}
}

func TestCloseRequiresExplicitID(t *testing.T) {
	paths := []string{}
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	for _, id := range []string{"", "/", "../configs", "not-a-uuid"} {
		if err := client.Close(context.Background(), id); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Close(%q): %v", id, err)
		}
	}
	if err := client.Close(context.Background(), "11111111-1111-4111-8111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/connections/11111111-1111-4111-8111-111111111111" {
		t.Fatalf("delete paths = %v", paths)
	}
}

func TestClientRejectsRedirectAndNeverLeaksUpstreamError(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetRequests.Add(1) }))
	defer target.Close()
	for _, code := range []int{http.StatusFound, http.StatusUnauthorized, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", target.URL)
				w.WriteHeader(code)
				fmt.Fprint(w, `{"message":"test-private-secret"}`)
			})
			_, err := client.Version(context.Background())
			if err == nil || strings.Contains(err.Error(), "test-private-secret") || strings.Contains(err.Error(), target.URL) {
				t.Fatalf("unsafe or missing error: %v", err)
			}
			if code == http.StatusNotFound && !errors.Is(err, ErrUnsupported) {
				t.Errorf("unsupported: %v", err)
			}
			if code == http.StatusUnauthorized && !errors.Is(err, ErrUnauthorized) {
				t.Errorf("unauthorized: %v", err)
			}
		})
	}
	if targetRequests.Load() != 0 {
		t.Fatal("client followed controller redirect")
	}
}

func TestClientBoundsResponseAndContext(t *testing.T) {
	t.Run("response size", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, strings.Repeat(" ", maxResponseBytes+1))
		})
		if _, err := client.Proxies(context.Background()); err == nil {
			t.Fatal("oversized response accepted")
		}
	})
	t.Run("request deadline", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		defer cancel()
		_, err := client.Version(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline: %v", err)
		}
	})
	t.Run("fixed timeout", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		if client.http.Timeout != requestTimeout || requestTimeout > 15*time.Second {
			t.Fatal("production client must have a bounded request timeout")
		}
		localHTTP := *client.http
		localHTTP.Timeout = 25 * time.Millisecond
		client.http = &localHTTP
		if _, err := client.Version(context.Background()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("client timeout = %v", err)
		}
	})
	t.Run("unsupported schema", func(t *testing.T) {
		client := testClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{}`) })
		if _, err := client.Proxies(context.Background()); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("schema: %v", err)
		}
	})
}
