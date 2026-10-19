// Package ctl 实现 `sbm ctl`：通过 Agent API（HTTPS + Bearer 令牌）操作管理器。
// 输出始终是 JSON，退出码见 exitCode。
package ctl

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 退出码：脚本与 Agent 依此分支。
const (
	ExitOK       = 0
	ExitUsage    = 1
	ExitInvalid  = 2 // 服务端拒绝：参数或校验失败
	ExitDecision = 3 // 需要决策：存在未应用改动、等待确认、配置已变化或需人工恢复
	ExitAuth     = 4 // 令牌无效、过期、吊销或权限不足
	ExitServer   = 5 // 网络或服务端错误
)

var decisionCodes = map[string]bool{"operation_pending": true, "pending_changes": true, "pending_mismatch": true, "base_changed": true, "recovery_required": true, "candidate_changed": true, "expired": true, "gateway_change_forbidden": true, "gateway_not_ready": true, "journal_unavailable": true}

type options struct {
	server, tokenFile, caFile, fingerprint string
	token                                  string // 已保存连接或首次交互输入，不接受命令行明文令牌
	timeout                                time.Duration
	allowHTTP                              bool
	stderr                                 io.Writer
}

type client struct {
	base  *url.URL
	token string
	http  *http.Client
}

type env struct {
	stdout, stderr io.Writer
	stdin          io.Reader
	getenv         func(string) string
}

const usage = `用法: sbm ctl [全局参数] <命令> [参数]

全局参数（也可用环境变量）:
  --server URL          管理器地址，默认使用已保存连接（SBM_SERVER）；非回环地址默认要求 https
  --token-file PATH     Agent 令牌文件，权限须为 600（SBM_TOKEN_FILE，默认 ~/.config/sbm/token）
  --ca-file PATH        自签证书的 CA（SBM_CA_FILE）
  --fingerprint HEX     固定服务端证书 SHA-256 指纹（SBM_FINGERPRINT）
  --timeout DURATION    请求超时，默认 60s
  --allow-insecure-http 允许以明文 HTTP 连接非回环地址（对应管理器的 -allow-insecure-http，仅限可信网络；SBM_ALLOW_INSECURE_HTTP=1）

首次接入:
  connect --server URL [--fingerprint HEX]  粘贴令牌后验证并保存连接，以后直接运行 sbm ctl status

只读:
  capabilities | status | diff | versions | audit [--limit N]
  explain domain <域名> [--source IP] | explain client <IP>
  logs [--source singbox|app] [--lines N] [--grep 文本]
  dns [--domain 文本] [--client IP] [--limit N]
  get <资源>              资源: rule rule_group filter subscription manual_node host split_dns device_group device dns blocklist imported_rule
  nodes [--country CC] | rules | proxies | connections | kernel
  gateway status|clients|plan
运行态（runtime 权限）:
  select <选择组> <节点> | delay <节点...> [--url U] [--timeout-ms N]
变更集（write 创建，deploy 应用/确认/回滚）:
  change create --file ops.json|- [--title T] [--include-pending DIGEST]
  op <create|update|delete|refresh|add|remove> <资源> [ID] [--value JSON | --value-file PATH|-]
  rule add --name N --type T --value V... --outbound O [--priority P] [--source CIDR...]
  rule delete <ID> | rule enable <ID> | rule disable <ID>
  block add <域名...> | block remove <域名...>
  sub add --name N --url-file PATH|- | sub refresh <ID> | sub delete <ID>
  node add --url-file PATH|- | node delete <ID>
  change list | change show <ID>
  change apply <ID> [--confirm-within 120] | change confirm <ID> | change rollback <ID>
  change revert <ID>     撤销已确认的变更（生成新的撤销变更集，同样需要 apply + confirm）
  以上创建类命令只创建并校验变更集，不会修改运行配置；需要 change apply 再 change confirm。
  创建/应用支持 --idempotency-key K，重试同一操作时使用相同的 K。

退出码: 0 成功；1 用法；2 校验失败；3 需要决策（未应用改动/等待确认/配置已变化/需人工恢复/apply_ready=false）；4 认证或权限；5 网络或服务端错误
`

// Main 返回进程退出码。
func Main(args []string) int {
	return Run(args, os.Stdout, os.Stderr, os.Stdin, os.Getenv)
}

// Run 便于测试注入输入输出与环境变量。
func Run(args []string, stdout, stderr io.Writer, stdin io.Reader, getenv func(string) string) int {
	return run(args, env{stdout: stdout, stderr: stderr, stdin: stdin, getenv: getenv})
}

func run(args []string, e env) int {
	// connect 也支持把连接参数写在子命令后，便于直接粘贴页面生成的命令。
	if len(args) > 0 && args[0] == "connect" {
		args = append(append([]string{}, args[1:]...), "connect")
	}
	opts := options{}
	global := flag.NewFlagSet("sbm ctl", flag.ContinueOnError)
	global.SetOutput(e.stderr)
	global.Usage = func() { fmt.Fprint(e.stderr, usage) }
	home, _ := os.UserHomeDir()
	defaultToken := filepath.Join(home, ".config", "sbm", "token")
	if xdg := e.getenv("XDG_CONFIG_HOME"); xdg != "" {
		defaultToken = filepath.Join(xdg, "sbm", "token")
	}
	global.StringVar(&opts.server, "server", firstNonEmpty(e.getenv("SBM_SERVER"), "http://127.0.0.1:9090"), "")
	global.StringVar(&opts.tokenFile, "token-file", firstNonEmpty(e.getenv("SBM_TOKEN_FILE"), defaultToken), "")
	global.StringVar(&opts.caFile, "ca-file", e.getenv("SBM_CA_FILE"), "")
	global.StringVar(&opts.fingerprint, "fingerprint", e.getenv("SBM_FINGERPRINT"), "")
	global.DurationVar(&opts.timeout, "timeout", 60*time.Second, "")
	global.BoolVar(&opts.allowHTTP, "allow-insecure-http", e.getenv("SBM_ALLOW_INSECURE_HTTP") == "1", "")
	opts.stderr = e.stderr
	if err := global.Parse(args); err != nil {
		return ExitUsage
	}
	rest := global.Args()
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "-h" {
		fmt.Fprint(e.stdout, usage)
		return ExitOK
	}
	explicit := map[string]bool{}
	global.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	for flag, variable := range map[string]string{"server": "SBM_SERVER", "token-file": "SBM_TOKEN_FILE", "ca-file": "SBM_CA_FILE", "fingerprint": "SBM_FINGERPRINT", "allow-insecure-http": "SBM_ALLOW_INSECURE_HTTP"} {
		explicit[flag] = explicit[flag] || e.getenv(variable) != ""
	}
	if rest[0] == "connect" {
		if len(rest) != 1 || !explicit["server"] {
			return usageErr(e, "请复制管理页面生成的 connect 命令（需要 --server 地址）")
		}
		return connect(opts, explicit, e)
	}
	if err := applySavedConnection(&opts, explicit, connectionPath(e)); err != nil {
		return fail(e, ExitUsage, err)
	}
	c, err := newClient(opts)
	if err != nil {
		return fail(e, ExitUsage, err)
	}
	return dispatch(c, rest, e)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func fail(e env, code int, err error) int {
	out, _ := json.MarshalIndent(map[string]any{"error": err.Error(), "code": "client_error"}, "", "  ")
	fmt.Fprintln(e.stdout, string(out))
	fmt.Fprintln(e.stderr, "sbm ctl:", err)
	return code
}

func newClient(o options) (*client, error) {
	base, err := url.Parse(strings.TrimRight(o.server, "/"))
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("--server 需要 http(s)://主机:端口")
	}
	host := base.Hostname()
	ip := net.ParseIP(host)
	if base.Scheme == "http" && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		if !o.allowHTTP {
			return nil, fmt.Errorf("非回环地址默认要求 https；远程主机可用 ssh -L 隧道后访问 http://127.0.0.1，可信局域网可显式加 --allow-insecure-http")
		}
		fmt.Fprintln(o.stderr, "sbm ctl: 警告：以明文 HTTP 连接非回环地址，Agent 令牌与配置未加密，请仅在可信网络使用")
	}
	token := o.token
	if token == "" {
		token, err = readTokenFile(o.tokenFile)
		if err != nil {
			return nil, err
		}
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	if o.caFile != "" {
		pem, err := os.ReadFile(o.caFile)
		if err != nil {
			return nil, fmt.Errorf("读取 CA 文件失败")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA 文件不是有效 PEM")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	if o.fingerprint != "" {
		want := strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(o.fingerprint))
		if len(want) != 64 {
			return nil, fmt.Errorf("--fingerprint 需要 64 位十六进制 SHA-256")
		}
		// 指纹固定替代 CA 链校验，适用于对管理 IP 自签的证书。
		transport.TLSClientConfig.InsecureSkipVerify = true
		transport.TLSClientConfig.VerifyPeerCertificate = func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return fmt.Errorf("服务端未提供证书")
			}
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != want {
				return fmt.Errorf("服务端证书指纹不匹配")
			}
			return nil
		}
	}
	return &client{base: base, token: token, http: &http.Client{
		Transport: transport, Timeout: o.timeout,
		// 不跟随重定向，避免把令牌带到其他地址。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("找不到令牌文件 %s：在管理界面「设置 → Agent 令牌」创建后保存为该文件并 chmod 600", path)
	}
	if info.Mode().Perm()&0077 != 0 {
		return "", fmt.Errorf("令牌文件 %s 权限过宽（%04o），请执行 chmod 600", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取令牌文件失败")
	}
	token := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(token, "sbm_") {
		return "", fmt.Errorf("令牌文件内容无效")
	}
	return token, nil
}

type response struct {
	status int
	body   []byte
}

func (c *client) do(method, path string, query url.Values, body any, idemKey string) (*response, error) {
	u := *c.base
	u.Path = strings.TrimRight(c.base.Path, "/") + "/api/agent/v1" + path
	if query != nil {
		u.RawQuery = query.Encode()
	}
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	// 只有带幂等键的写请求在网络错误时重试，服务端返回已记录结果。
	attempts := 1
	if idemKey != "" {
		attempts = 3
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequest(method, u.String(), bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idemKey != "" {
			req.Header.Set("Idempotency-Key", idemKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return &response{status: resp.StatusCode, body: data}, nil
	}
	return nil, fmt.Errorf("请求失败：%s", redactURL(lastErr))
}

func redactURL(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	if err == nil {
		return "未知错误"
	}
	return err.Error()
}

func newKey() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "ctl-" + hex.EncodeToString(b)
}

// emit 打印 JSON 并按状态码与错误码映射退出码。
func emit(e env, resp *response, err error) int {
	if err != nil {
		return fail(e, ExitServer, err)
	}
	var payload map[string]any
	if json.Unmarshal(resp.body, &payload) != nil {
		payload = map[string]any{"error": strings.TrimSpace(string(resp.body)), "status": resp.status}
	}
	out, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Fprintln(e.stdout, string(out))
	code, _ := payload["code"].(string)
	switch {
	case resp.status >= 200 && resp.status < 300:
		if data, ok := payload["data"].(map[string]any); ok && data["apply_ready"] == false {
			fmt.Fprintln(e.stderr, "sbm ctl: 变更集已创建但不能直接应用：", data["blocked_reason"])
			return ExitDecision
		}
		return ExitOK
	case resp.status == 401 || resp.status == 403:
		return ExitAuth
	case decisionCodes[code] || resp.status == 409:
		return ExitDecision
	case resp.status >= 500 || resp.status == 429:
		return ExitServer
	}
	return ExitInvalid
}

func readValue(e env, inline, file string) (json.RawMessage, error) {
	if inline != "" && file != "" {
		return nil, fmt.Errorf("--value 与 --value-file 只能选一个")
	}
	raw := []byte(inline)
	if file != "" {
		var err error
		if raw, err = readInput(e, file); err != nil {
			return nil, err
		}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("value 不是有效 JSON")
	}
	return json.RawMessage(raw), nil
}

func readInput(e env, path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(io.LimitReader(e.stdin, 4<<20))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败", path)
	}
	return raw, nil
}
