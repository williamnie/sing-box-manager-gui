package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/ctl"
)

// TestCtlEndToEnd 通过真实路由与 sbm ctl 客户端走完整变更闭环。
func TestCtlEndToEnd(t *testing.T) {
	f := newAgentFixture(t)
	srv := httptest.NewServer(f.s.router)
	defer srv.Close()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenPath, []byte(f.token), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(stdin string, args ...string) (int, map[string]any) {
		var out, errOut bytes.Buffer
		code := ctl.Run(append([]string{"--server", srv.URL, "--token-file", tokenPath}, args...), &out, &errOut, strings.NewReader(stdin), func(string) string { return "" })
		var payload map[string]any
		_ = json.Unmarshal(out.Bytes(), &payload)
		return code, payload
	}
	id := func(p map[string]any) string { return p["data"].(map[string]any)["id"].(string) }
	state := func(p map[string]any) string { return p["data"].(map[string]any)["state"].(string) }

	if code, p := run("", "status"); code != 0 || p["data"] == nil {
		t.Fatal("status", code, p)
	}
	code, p := run("", "rule", "add", "--name", "corp", "--type", "domain_suffix", "--value", "corp.example", "--outbound", "DIRECT")
	if code != 0 || state(p) != csChecked {
		t.Fatal("rule add", code, p)
	}
	cs := id(p)
	if code, p := run("", "change", "apply", cs, "--confirm-within", "60"); code != 0 || state(p) != csPending {
		t.Fatal("apply", code, p)
	}
	// 确认窗口内再建一个变更并尝试应用，应返回需要决策。
	_, p = run("", "block", "add", "ads.example")
	if code, _ := run("", "change", "apply", id(p)); code != ctl.ExitDecision {
		t.Fatal("second apply during window", code)
	}
	if code, p := run("", "change", "confirm", cs); code != 0 || state(p) != csCommitted {
		t.Fatal("confirm", code, p)
	}
	if code, p := run("", "explain", "domain", "www.corp.example"); code != 0 || p["data"].(map[string]any)["applied"].(map[string]any)["outbound"] != "DIRECT" {
		t.Fatal("explain", code, p)
	}
	// 订阅 URL 从 stdin 读取，输出不含凭据。
	code, p = run("https://sub.example.net/s?token=very-secret-1", "sub", "add", "--name", "main", "--url-file", "-")
	raw, _ := json.Marshal(p)
	if code != 0 || strings.Contains(string(raw), "very-secret-1") {
		t.Fatal("sub add", code, string(raw))
	}
	sub := id(p)
	if code, p := run("", "change", "apply", sub); code != 0 || state(p) != csPending {
		t.Fatal("apply sub", code, p)
	}
	if code, p := run("", "change", "rollback", sub); code != 0 || state(p) != csRolledBack {
		t.Fatal("rollback", code, p)
	}
	if len(f.s.store.GetSubscriptions()) != 0 {
		t.Fatal("rollback kept subscription")
	}
	if code, p := run("", "audit", "--limit", "5"); code != 0 || len(p["data"].([]any)) == 0 {
		t.Fatal("audit", code, p)
	}

	// 只读令牌写入返回权限退出码。
	w := request(f.s, "POST", "/api/agent/tokens", map[string]any{"name": "ro", "scopes": []string{"read"}, "days": 1}, f.cookie, "")
	var created struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if err := os.WriteFile(tokenPath, []byte(created.Token), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := run("", "rule", "delete", "x"); code != ctl.ExitAuth {
		t.Fatal("read-only token wrote", code)
	}
}
