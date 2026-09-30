package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/runtimecontrol"
)

type runtimeFixtureProcess struct {
	*fakeProcess
	mu     sync.RWMutex
	target daemon.RuntimeTarget
}

func (p *runtimeFixtureProcess) WithRuntime(ctx context.Context, fn func(daemon.RuntimeTarget) error) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(p.target)
}
func (p *runtimeFixtureProcess) replaceInstance(instance string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target.Instance = instance
}
func runtimeAPIFixture(t *testing.T, handler http.HandlerFunc) (*Server, *runtimeFixtureProcess, *http.Cookie) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	s := testServer(t)
	cookie := setup(t, s)
	process := &runtimeFixtureProcess{fakeProcess: &fakeProcess{running: true}, target: daemon.RuntimeTarget{Instance: "active-instance", Controller: strings.TrimPrefix(upstream.URL, "http://"), Secret: "applied-secret"}}
	s.processManager = process
	return s, process, cookie
}
func decodeRuntimeData[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var response struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestRuntimeAPIAuthenticationAndRequestBoundary(t *testing.T) {
	var calls atomic.Int32
	s, _, cookie := runtimeAPIFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		t.Errorf("rejected request reached upstream: %s", r.URL.Path)
	})
	for _, item := range []struct {
		method, path string
		data         any
	}{{"GET", "/api/runtime/proxies", nil}, {"GET", "/api/runtime/connections", nil}, {"POST", "/api/runtime/select", map[string]any{"group": "Proxy", "member": "DIRECT", "instance": "active-instance"}}, {"POST", "/api/runtime/delay", map[string]any{"tags": []string{"DIRECT"}, "url": "https://1.1.1.1/test", "timeout": 100, "instance": "active-instance"}}, {"POST", "/api/runtime/connections/close", map[string]any{"ids": []string{"id"}, "instance": "active-instance"}}} {
		if w := request(s, item.method, item.path, item.data, nil, ""); w.Code != 401 {
			t.Errorf("%s unauthenticated=%d", item.path, w.Code)
		}
		if w := request(s, item.method, item.path, item.data, cookie, "https://evil.example"); w.Code != 403 {
			t.Errorf("%s cross-origin=%d", item.path, w.Code)
		}
		req := httptest.NewRequest(item.method, "http://evil.example"+item.path, nil)
		req.RemoteAddr = "127.0.0.1:51234"
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)
		if w.Code != 403 {
			t.Errorf("%s Host rebinding=%d", item.path, w.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("rejected requests contacted upstream")
	}
}

func TestRuntimeAPIUsesAppliedTargetAndKeepsSecretsPrivate(t *testing.T) {
	var activeCalls, draftCalls atomic.Int32
	draft := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { draftCalls.Add(1); http.Error(w, "wrong target", 500) }))
	defer draft.Close()
	s, _, cookie := runtimeAPIFixture(t, func(w http.ResponseWriter, r *http.Request) {
		activeCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer applied-secret" {
			t.Error("runtime did not use applied secret")
		}
		switch r.URL.Path {
		case "/version":
			fmt.Fprint(w, `{"version":"1.14-fixture"}`)
		case "/proxies":
			fmt.Fprint(w, `{"proxies":{"DIRECT":{"type":"Direct"}}}`)
		default:
			http.NotFound(w, r)
		}
	})
	draftURL, _ := url.Parse(draft.URL)
	_, port, _ := net.SplitHostPort(draftURL.Host)
	number, _ := strconv.Atoi(port)
	settings := s.store.GetSettings()
	settings.ClashAPIPort = number
	settings.ClashAPISecret = "draft-secret"
	if err := s.store.UpdateSettings(settings); err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/runtime/proxies", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data := decodeRuntimeData[proxySnapshot](t, w)
	if data.Instance != "active-instance" || data.Version != "1.14-fixture" || len(data.Proxies) != 1 || activeCalls.Load() != 2 || draftCalls.Load() != 0 {
		t.Fatalf("wrong runtime snapshot or target: %#v active=%d draft=%d", data, activeCalls.Load(), draftCalls.Load())
	}
	if strings.Contains(w.Body.String(), "applied-secret") || strings.Contains(w.Body.String(), "draft-secret") {
		t.Fatal("runtime response exposed secret")
	}
}

func TestRuntimeAPIRejectsStaleInstanceBeforeAnyUpstreamRequest(t *testing.T) {
	var calls atomic.Int32
	s, _, cookie := runtimeAPIFixture(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, item := range []struct {
		path string
		body map[string]any
	}{{"select", map[string]any{"group": "Proxy", "member": "DIRECT", "instance": "old-instance"}}, {"delay", map[string]any{"tags": []string{"DIRECT"}, "url": "https://1.1.1.1/test", "timeout": 100, "instance": "old-instance"}}, {"connections/close", map[string]any{"ids": []string{"7ab30f10-7c6a-4d8f-81ce-d15f196e5331"}, "instance": "old-instance"}}} {
		w := request(s, "POST", "/api/runtime/"+item.path, item.body, cookie, "")
		if w.Code != 409 {
			t.Errorf("%s stale instance=%d %s", item.path, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("stale instance reached upstream")
	}
}

func TestRuntimeAPISelectorValidationSpecialTagsAndReadback(t *testing.T) {
	group := "代理 /?#%中文"
	member := "香港 /?#%节点"
	var mu sync.Mutex
	selected := "DIRECT"
	ignoreWrite := false
	writes := 0
	proxyReads := 0
	s, _, cookie := runtimeAPIFixture(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/version":
			fmt.Fprint(w, `{"version":"fixture"}`)
		case r.URL.Path == "/proxies":
			proxyReads++
			_ = json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{group: map[string]any{"type": "Selector", "all": []string{"DIRECT", member}, "now": selected}, "自动": map[string]any{"type": "URLTest", "all": []string{member}, "now": member}, member: map[string]any{"type": "Shadowsocks"}, "DIRECT": map[string]any{"type": "Direct"}}})
		case r.Method == "PUT":
			writes++
			if r.URL.EscapedPath() != "/proxies/"+url.PathEscape(group) || r.URL.RawQuery != "" {
				t.Errorf("special tag escaped incorrectly: %s", r.URL.String())
			}
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body.Name != member {
				t.Errorf("member=%q", body.Name)
			}
			if !ignoreWrite {
				selected = body.Name
			}
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	})
	for _, body := range []map[string]any{{"group": group, "member": "not-a-member", "instance": "active-instance"}, {"group": "自动", "member": member, "instance": "active-instance"}} {
		w := request(s, "POST", "/api/runtime/select", body, cookie, "")
		if w.Code != 400 {
			t.Fatalf("invalid selector accepted: %d %s", w.Code, w.Body.String())
		}
	}
	mu.Lock()
	before := writes
	mu.Unlock()
	if before != 0 {
		t.Fatal("invalid selection caused write")
	}
	body := map[string]any{"group": group, "member": member, "instance": "active-instance"}
	w := request(s, "POST", "/api/runtime/select", body, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data := decodeRuntimeData[proxySnapshot](t, w)
	confirmed := false
	for _, proxy := range data.Proxies {
		if proxy.Tag == group {
			confirmed = proxy.Selected == member
		}
	}
	if !confirmed {
		t.Fatal("response not read back from runtime")
	}
	mu.Lock()
	writesAfter := writes
	readsAfter := proxyReads
	selected = "DIRECT"
	ignoreWrite = true
	mu.Unlock()
	if writesAfter != 1 || readsAfter < 5 {
		t.Fatalf("selection did not validate and read back: writes=%d reads=%d", writesAfter, readsAfter)
	}
	w = request(s, "POST", "/api/runtime/select", body, cookie, "")
	if w.Code != 409 {
		t.Fatalf("ignored upstream selection reported success: %d %s", w.Code, w.Body.String())
	}
}

func TestRuntimeAPIConnectionCloseReportsMissingAndPartialFailure(t *testing.T) {
	closed := "7ab30f10-7c6a-4d8f-81ce-d15f196e5331"
	failed := "7ab30f10-7c6a-4d8f-81ce-d15f196e5332"
	missing := "7ab30f10-7c6a-4d8f-81ce-d15f196e5333"
	var mu sync.Mutex
	deleted := []string{}
	s, _, cookie := runtimeAPIFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/connections" {
			_ = json.NewEncoder(w).Encode(map[string]any{"uploadTotal": 0, "downloadTotal": 0, "connections": []any{map[string]any{"id": closed}, map[string]any{"id": failed}}})
			return
		}
		if r.Method == "DELETE" {
			id := strings.TrimPrefix(r.URL.Path, "/connections/")
			mu.Lock()
			deleted = append(deleted, id)
			mu.Unlock()
			if id == failed {
				http.Error(w, "failure", 500)
			} else {
				w.WriteHeader(204)
			}
			return
		}
		http.NotFound(w, r)
	})
	w := request(s, "POST", "/api/runtime/connections/close", map[string]any{"ids": []string{closed, failed, missing, closed}, "instance": "active-instance"}, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	data := decodeRuntimeData[closeResult](t, w)
	if !reflect.DeepEqual(data, closeResult{Closed: []string{closed}, Failed: []string{failed}, Missing: []string{missing}}) {
		t.Fatalf("inaccurate partial result: %#v", data)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(deleted, []string{closed, failed}) {
		t.Fatalf("closed missing/duplicate or all connections: %v", deleted)
	}
}

func TestRuntimeDelayTargetsCyclesDeduplicateAndBoundExpansion(t *testing.T) {
	proxies := []runtimecontrol.Proxy{{Tag: "A", Members: []string{"B", "node"}}, {Tag: "B", Members: []string{"A", "node", "other"}}, {Tag: "node"}, {Tag: "other"}}
	tags, err := delayTargets(proxies, []string{"A", "node"})
	if err != nil || !reflect.DeepEqual(tags, []string{"node", "other"}) {
		t.Fatalf("cyclic groups: %v %v", tags, err)
	}
	if _, err := delayTargets(proxies, []string{"missing"}); !errors.Is(err, runtimecontrol.ErrNotFound) {
		t.Fatalf("missing target: %v", err)
	}
	many := []runtimecontrol.Proxy{{Tag: "root"}}
	for i := 0; i < 513; i++ {
		tag := fmt.Sprintf("node-%d", i)
		many[0].Members = append(many[0].Members, tag)
		many = append(many, runtimecontrol.Proxy{Tag: tag})
	}
	if _, err := delayTargets(many, []string{"root"}); !errors.Is(err, runtimecontrol.ErrInvalidRequest) {
		t.Fatalf("unbounded expansion accepted: %v", err)
	}
}

func TestRuntimeAPIQueuedDelayRechecksNewInstance(t *testing.T) {
	proxiesRead := make(chan struct{}, 1)
	var probes atomic.Int32
	s, process, cookie := runtimeAPIFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxies" {
			fmt.Fprint(w, `{"proxies":{"DIRECT":{"type":"Direct"}}}`)
			proxiesRead <- struct{}{}
			return
		}
		if strings.HasSuffix(r.URL.Path, "/delay") {
			probes.Add(1)
			fmt.Fprint(w, `{"delay":1}`)
			return
		}
		http.NotFound(w, r)
	})
	for i := 0; i < cap(s.runtimeDelaySlots); i++ {
		s.runtimeDelaySlots <- struct{}{}
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(s, "POST", "/api/runtime/delay", map[string]any{"tags": []string{"DIRECT"}, "url": "https://1.1.1.1/test", "timeout": 100, "instance": "active-instance"}, cookie, "")
	}()
	select {
	case <-proxiesRead:
	case <-time.After(2 * time.Second):
		t.Fatal("delay never read initial runtime")
	}
	process.replaceInstance("replacement-instance")
	for i := 0; i < cap(s.runtimeDelaySlots); i++ {
		<-s.runtimeDelaySlots
	}
	select {
	case w := <-done:
		if w.Code != 409 {
			t.Fatalf("queued stale delay=%d %s", w.Code, w.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued delay did not finish")
	}
	if probes.Load() != 0 {
		t.Fatal("queued delay reached replacement instance")
	}
}
