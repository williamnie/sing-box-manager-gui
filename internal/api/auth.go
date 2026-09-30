package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"golang.org/x/crypto/bcrypt"
)

type authSession struct{ Expires time.Time }
type loginLimit struct {
	Count int
	Since time.Time
}
type authManager struct {
	mu       sync.Mutex
	dir      string
	hash     []byte
	setup    string
	sessions map[[32]byte]authSession
	limits   map[string]loginLimit
}

func newAuth(dir string) (*authManager, error) {
	a := &authManager{dir: dir, sessions: map[[32]byte]authSession{}, limits: map[string]loginLimit{}}
	data, err := os.ReadFile(filepath.Join(dir, "auth.json"))
	if err == nil {
		var record struct {
			Hash string `json:"password_hash"`
		}
		if json.Unmarshal(data, &record) != nil || record.Hash == "" {
			return nil, fmt.Errorf("认证文件损坏")
		}
		if err := os.Chmod(filepath.Join(dir, "auth.json"), 0600); err != nil {
			return nil, err
		}
		a.hash = []byte(record.Hash)
		if _, e := bcrypt.Cost(a.hash); e != nil {
			return nil, fmt.Errorf("密码记录损坏")
		}
		return a, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	tokenPath := filepath.Join(dir, "setup-token")
	token, err := os.ReadFile(tokenPath)
	if os.IsNotExist(err) {
		value := make([]byte, 32)
		if _, err = rand.Read(value); err != nil {
			return nil, err
		}
		token = []byte(hex.EncodeToString(value))
		err = privateWrite(tokenPath, token)
	}
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(tokenPath, 0600); err != nil {
		return nil, err
	}
	a.setup = strings.TrimSpace(string(token))
	if len(a.setup) != 64 {
		return nil, fmt.Errorf("初始化令牌文件无效")
	}
	return a, nil
}
func privateWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".private-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	e := f.Close()
	if err != nil {
		return err
	}
	if e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func (s *Server) securityBoundary(c *gin.Context) {
	if len(s.allowedNetworks) > 0 {
		host, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
		ip := net.ParseIP(host)
		allowed := ip != nil && ip.IsLoopback()
		for _, network := range s.allowedNetworks {
			if network.Contains(ip) {
				allowed = true
			}
		}
		if !allowed {
			c.AbortWithStatusJSON(403, gin.H{"error": "来源地址未被允许"})
			return
		}
	}

	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Frame-Options", "DENY")
	c.Header("Referrer-Policy", "no-referrer")
	if strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.Header("Cache-Control", "no-store")
	}
	host := c.Request.Host
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	if host != "localhost" && net.ParseIP(strings.Trim(host, "[]")) == nil {
		c.AbortWithStatusJSON(403, gin.H{"error": "管理接口只接受 localhost 或明确的 IP 地址 Host"})
		return
	}
	if origin := c.GetHeader("Origin"); origin != "" {
		u, e := url.Parse(origin)
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		if e != nil || u.Host != c.Request.Host || u.Scheme != scheme {
			c.AbortWithStatusJSON(403, gin.H{"error": "跨站请求被拒绝"})
			return
		}
	}
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		c.AbortWithStatusJSON(403, gin.H{"error": "跨站请求被拒绝"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	c.Next()
}
func (a *authManager) authenticated(c *gin.Context) bool {
	token, e := c.Cookie("sbm_session")
	if e != nil {
		return false
	}
	key := sha256.Sum256([]byte(token))
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.sessions[key]
	if !ok || time.Now().After(v.Expires) {
		delete(a.sessions, key)
		return false
	}
	return true
}
func (s *Server) requireAuth(c *gin.Context) {
	if s.auth == nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "认证服务不可用"})
		return
	}
	if !s.auth.authenticated(c) {
		c.AbortWithStatusJSON(401, gin.H{"error": "请先登录"})
		return
	}
	c.Next()
}
func (s *Server) authStatus(c *gin.Context) {
	if s.auth == nil {
		c.JSON(503, gin.H{"error": "认证服务不可用"})
		return
	}
	s.auth.mu.Lock()
	setup := len(s.auth.hash) == 0
	s.auth.mu.Unlock()
	c.JSON(200, gin.H{"data": gin.H{"authenticated": s.auth.authenticated(c), "setup_required": setup}})
}
func (a *authManager) limited(ip string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, v := range a.limits {
		if now.Sub(v.Since) > time.Minute {
			delete(a.limits, k)
		}
	}
	v := a.limits[ip]
	if v.Count == 0 {
		v.Since = now
	}
	v.Count++
	if len(a.limits) > 1024 {
		return true
	}
	a.limits[ip] = v
	return v.Count > 5
}
func (a *authManager) session(c *gin.Context) error {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return err
	}
	token := hex.EncodeToString(data)
	a.mu.Lock()
	for k, v := range a.sessions {
		if time.Now().After(v.Expires) {
			delete(a.sessions, k)
		}
	}
	if len(a.sessions) >= 100 {
		a.sessions = map[[32]byte]authSession{}
	}
	a.sessions[sha256.Sum256([]byte(token))] = authSession{Expires: time.Now().Add(8 * time.Hour)}
	a.mu.Unlock()
	http.SetCookie(c.Writer, &http.Cookie{Name: "sbm_session", Value: token, Path: "/", MaxAge: 8 * 3600, HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode})
	return nil
}
func (s *Server) login(c *gin.Context) {
	if s.auth == nil {
		c.JSON(503, gin.H{"error": "认证不可用"})
		return
	}
	host, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	if s.auth.limited(host) {
		c.JSON(429, gin.H{"error": "尝试过于频繁，请一分钟后重试"})
		return
	}
	var req struct {
		Password   string `json:"password"`
		SetupToken string `json:"setup_token"`
	}
	if c.ShouldBindJSON(&req) != nil || len(req.Password) > 72 {
		c.JSON(400, gin.H{"error": "密码格式无效"})
		return
	}
	if c.FullPath() == "/api/auth/setup" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			c.JSON(403, gin.H{"error": "首次设置需在本机或 SSH 隧道完成"})
			return
		}
		if len(req.Password) < 12 {
			c.JSON(400, gin.H{"error": "密码至少需要 12 字符"})
			return
		}
		s.auth.mu.Lock()
		if len(s.auth.hash) > 0 || subtle.ConstantTimeCompare([]byte(req.SetupToken), []byte(s.auth.setup)) != 1 {
			s.auth.mu.Unlock()
			c.JSON(403, gin.H{"error": "初始化令牌无效或已完成初始化"})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err == nil {
			data, _ := json.Marshal(map[string]string{"password_hash": string(hash)})
			err = privateWrite(filepath.Join(s.auth.dir, "auth.json"), data)
		}
		if err == nil {
			s.auth.hash = hash
			s.auth.setup = ""
			_ = os.Remove(filepath.Join(s.auth.dir, "setup-token"))
		}
		s.auth.mu.Unlock()
		if err != nil {
			c.JSON(500, gin.H{"error": "保存密码失败"})
			return
		}
	} else {
		s.auth.mu.Lock()
		hash := append([]byte{}, s.auth.hash...)
		s.auth.mu.Unlock()
		if len(hash) == 0 || bcrypt.CompareHashAndPassword(hash, []byte(req.Password)) != nil {
			c.JSON(401, gin.H{"error": "密码错误"})
			return
		}
	}
	if err := s.auth.session(c); err != nil {
		c.JSON(500, gin.H{"error": "创建会话失败"})
		return
	}
	c.JSON(200, gin.H{"message": "已登录"})
}
func (s *Server) logout(c *gin.Context) {
	if s.auth != nil {
		token, _ := c.Cookie("sbm_session")
		s.auth.mu.Lock()
		delete(s.auth.sessions, sha256.Sum256([]byte(token)))
		s.auth.mu.Unlock()
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "sbm_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: c.Request.TLS != nil, SameSite: http.SameSiteStrictMode})
	c.JSON(200, gin.H{"message": "已退出"})
}

// AuthReady 在启动调度器或监听前提供失败关闭的初始化检查。
func (s *Server) AuthReady() bool                          { return s.auth != nil }
func (s *Server) SetAllowedNetworks(networks []*net.IPNet) { s.allowedNetworks = networks }
func (s *Server) RunTLS(addr, cert, key string) error {
	return s.httpServer(addr).ListenAndServeTLS(cert, key)
}

func (s *Server) httpServer(addr string) *http.Server {
	return &http.Server{Addr: addr, Handler: s.router, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}

// 不使用会转储请求头的默认 recovery，避免 Cookie/订阅内容进入错误日志。
func (s *Server) recoverRequest(c *gin.Context) {
	defer func() {
		if recover() != nil {
			logger.Printf("管理请求内部处理失败")
			c.AbortWithStatusJSON(500, gin.H{"error": "内部处理失败"})
		}
	}()
	c.Next()
}
