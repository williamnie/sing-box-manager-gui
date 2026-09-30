package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type request struct {
	Role   string `json:"role"`
	Config Config `json:"config"`
}
type response struct {
	Plan   *Plan        `json:"plan,omitempty"`
	Check  *CheckResult `json:"check,omitempty"`
	Status *Status      `json:"status,omitempty"`
	Error  string       `json:"error,omitempty"`
}

// Handler 只有固定操作集合，无命令、路径、服务名或原始 nftables 输入字段。
func Handler(m *Manager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		result := response{}
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(response{Error: "仅支持 POST"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var req request
		if e := decoder.Decode(&req); e != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(response{Error: "请求格式无效或超出大小限制"})
			return
		}
		var extra any
		if e := decoder.Decode(&extra); e != io.EOF {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(response{Error: "请求只能包含一个 JSON 对象"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		var e error
		switch r.URL.Path {
		case "/preview":
			var v Plan
			v, e = Preview(req.Role, req.Config)
			result.Plan = &v
		case "/check":
			var v CheckResult
			v, e = m.Check(ctx, req.Role, req.Config)
			result.Check = &v
		case "/apply":
			var v Status
			v, e = m.Apply(ctx, req.Role, req.Config)
			result.Status = &v
		case "/restart-manager":
			var v Status
			v, e = m.RestartManager(ctx)
			result.Status = &v
		case "/status":
			var v Status
			v, e = m.Status(ctx)
			result.Status = &v
		case "/rollback":
			var v Status
			v, e = m.Rollback(ctx)
			result.Status = &v
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(response{Error: "未知操作"})
			return
		}
		if e != nil {
			result.Error = e.Error()
			w.WriteHeader(http.StatusConflict)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func Serve(ctx context.Context, m *Manager, socket string, gid int) error {
	if m.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("辅助服务必须在 Linux 上由 root 启动")
	}
	if filepath.Clean(socket) != DefaultSocket {
		return fmt.Errorf("辅助服务仅允许固定 socket 路径 %s", DefaultSocket)
	}
	if e := trustedPath(m.StateDir, true); e != nil {
		return e
	}
	if e := trustedPath(filepath.Dir(socket), true); e != nil {
		return e
	}
	if info, e := os.Lstat(socket); e == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket 路径已有非 socket 文件")
		}
		connection, connectErr := net.DialTimeout("unix", socket, time.Second)
		if connectErr == nil {
			connection.Close()
			return fmt.Errorf("辅助服务已在运行")
		}
		if e = os.Remove(socket); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	listener, e := net.Listen("unix", socket)
	if e != nil {
		return e
	}
	defer listener.Close()
	defer os.Remove(socket)
	if e = os.Chown(socket, 0, gid); e != nil {
		return e
	}
	if e = os.Chmod(socket, 0660); e != nil {
		return e
	}
	server := &http.Server{Handler: Handler(m), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 70 * time.Second, IdleTimeout: 15 * time.Second, MaxHeaderBytes: 4096}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	e = server.Serve(listener)
	if errors.Is(e, http.ErrServerClosed) {
		return nil
	}
	return e
}

type Client struct{ http *http.Client }

func NewClient(socket string) *Client {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}, MaxIdleConns: 2, IdleConnTimeout: 15 * time.Second}
	return &Client{http: &http.Client{Transport: transport, Timeout: 70 * time.Second}}
}
func (c *Client) call(ctx context.Context, path, role string, config Config) (response, error) {
	b, e := json.Marshal(request{role, config})
	if e != nil {
		return response{}, e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", "http://unix"+path, strings.NewReader(string(b)))
	if e != nil {
		return response{}, e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return response{}, fmt.Errorf("网关辅助服务不可用: %w", e)
	}
	defer resp.Body.Close()
	var r response
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); e != nil {
		return r, fmt.Errorf("辅助服务响应格式无效")
	}
	if r.Error != "" {
		return r, errors.New(r.Error)
	}
	if resp.StatusCode != 200 {
		return r, fmt.Errorf("辅助服务返回状态 %d", resp.StatusCode)
	}
	return r, nil
}
func (c *Client) Preview(ctx context.Context, role string, config Config) (Plan, error) {
	r, e := c.call(ctx, "/preview", role, config)
	if r.Plan != nil {
		return *r.Plan, e
	}
	return Plan{}, e
}
func (c *Client) Check(ctx context.Context, role string, config Config) (CheckResult, error) {
	r, e := c.call(ctx, "/check", role, config)
	if r.Check != nil {
		return *r.Check, e
	}
	return CheckResult{}, e
}
func (c *Client) Apply(ctx context.Context, role string, config Config) (Status, error) {
	r, e := c.call(ctx, "/apply", role, config)
	if r.Status != nil {
		return *r.Status, e
	}
	return Status{}, e
}
func (c *Client) Status(ctx context.Context) (Status, error) {
	r, e := c.call(ctx, "/status", "", Config{})
	if r.Status != nil {
		return *r.Status, e
	}
	return Status{}, e
}
func (c *Client) Rollback(ctx context.Context) (Status, error) {
	r, e := c.call(ctx, "/rollback", "", Config{})
	if r.Status != nil {
		return *r.Status, e
	}
	return Status{}, e
}

func (c *Client) RestartManager(ctx context.Context) (Status, error) {
	r, e := c.call(ctx, "/restart-manager", "", Config{})
	if r.Status != nil {
		return *r.Status, e
	}
	return Status{}, e
}
