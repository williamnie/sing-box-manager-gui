package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// Agent 令牌独立于管理密码：只能访问 /api/agent/v1，不能管理令牌或改密。
const (
	scopeRead    = "read"
	scopeRuntime = "runtime"
	scopeWrite   = "write"
	scopeDeploy  = "deploy"

	maxAgentTokens      = 50
	maxAgentTokenDays   = 90
	agentTokenRetention = 30 * 24 * time.Hour
)

var agentScopes = []string{scopeRead, scopeRuntime, scopeWrite, scopeDeploy}

type agentToken struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Hash       string     `json:"hash"`
	Scopes     []string   `json:"scopes"`
	CIDRs      []string   `json:"cidrs,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// 返回给界面的视图不含摘要。
type agentTokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	CIDRs      []string   `json:"cidrs"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	Status     string     `json:"status"`
}

type agentTokenStore struct {
	mu     sync.Mutex
	path   string
	tokens []agentToken
}

func newAgentTokenStore(dir string) (*agentTokenStore, error) {
	s := &agentTokenStore{path: filepath.Join(dir, "agent-tokens.json")}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.tokens); err != nil || len(s.tokens) > maxAgentTokens*4 {
		return nil, fmt.Errorf("Agent 令牌文件损坏")
	}
	for _, t := range s.tokens {
		if raw, e := hex.DecodeString(t.Hash); e != nil || len(raw) != sha256.Size || t.ID == "" {
			return nil, fmt.Errorf("Agent 令牌记录损坏")
		}
	}
	if err := os.Chmod(s.path, 0600); err != nil {
		return nil, err
	}
	return s, nil
}

func (t agentToken) status(now time.Time) string {
	switch {
	case t.RevokedAt != nil:
		return "revoked"
	case !now.Before(t.ExpiresAt):
		return "expired"
	}
	return "active"
}

func (t agentToken) view(now time.Time) agentTokenView {
	cidrs := t.CIDRs
	if cidrs == nil {
		cidrs = []string{}
	}
	return agentTokenView{ID: t.ID, Name: t.Name, Scopes: t.Scopes, CIDRs: cidrs, CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, RevokedAt: t.RevokedAt, LastUsedAt: t.LastUsedAt, Status: t.status(now)}
}

func (t agentToken) has(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (s *agentTokenStore) saveLocked(tokens []agentToken) error {
	data, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	return privateWrite(s.path, data)
}

func normalizeTokenRequest(name string, scopes, cidrs []string, days int) (string, []string, []string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 || strings.ContainsAny(name, "\r\n\t") {
		return "", nil, nil, fmt.Errorf("令牌名称需为 1–64 个字符")
	}
	if days < 1 || days > maxAgentTokenDays {
		return "", nil, nil, fmt.Errorf("有效期需为 1–%d 天", maxAgentTokenDays)
	}
	set := map[string]bool{}
	for _, scope := range scopes {
		known := false
		for _, s := range agentScopes {
			known = known || s == scope
		}
		if !known {
			return "", nil, nil, fmt.Errorf("未知权限：%s", scope)
		}
		set[scope] = true
	}
	if len(set) == 0 {
		return "", nil, nil, fmt.Errorf("至少选择一个权限")
	}
	normalized := []string{}
	for _, s := range agentScopes {
		if set[s] {
			normalized = append(normalized, s)
		}
	}
	networks := []string{}
	for _, value := range cidrs {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			addr, e := netip.ParseAddr(value)
			if e != nil {
				return "", nil, nil, fmt.Errorf("来源网段无效：%s", value)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		networks = append(networks, prefix.Masked().String())
	}
	if len(networks) > 16 {
		return "", nil, nil, fmt.Errorf("来源网段最多 16 个")
	}
	return name, normalized, networks, nil
}

// create 返回仅此一次可见的明文令牌。
func (s *agentTokenStore) create(name string, scopes, cidrs []string, days int, now time.Time) (agentToken, string, error) {
	name, scopes, cidrs, err := normalizeTokenRequest(name, scopes, cidrs, days)
	if err != nil {
		return agentToken{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := []agentToken{}
	active := 0
	for _, t := range s.tokens {
		// 撤销或过期超过保留期的记录才清理，审计仍可对照令牌名称。
		end := t.ExpiresAt
		if t.RevokedAt != nil {
			end = *t.RevokedAt
		}
		if now.Sub(end) < agentTokenRetention {
			kept = append(kept, t)
		}
		if t.status(now) == "active" {
			active++
		}
	}
	if active >= maxAgentTokens {
		return agentToken{}, "", fmt.Errorf("有效令牌最多 %d 个，请先吊销不用的令牌", maxAgentTokens)
	}
	idRaw, secretRaw := make([]byte, 4), make([]byte, 32)
	if _, err := rand.Read(idRaw); err != nil {
		return agentToken{}, "", err
	}
	if _, err := rand.Read(secretRaw); err != nil {
		return agentToken{}, "", err
	}
	id := hex.EncodeToString(idRaw)
	plain := "sbm_" + id + "_" + hex.EncodeToString(secretRaw)
	sum := sha256.Sum256([]byte(plain))
	token := agentToken{ID: id, Name: name, Hash: hex.EncodeToString(sum[:]), Scopes: scopes, CIDRs: cidrs, CreatedAt: now.UTC(), ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour).UTC()}
	next := append(kept, token)
	if err := s.saveLocked(next); err != nil {
		return agentToken{}, "", err
	}
	s.tokens = next
	return token, plain, nil
}

func (s *agentTokenStore) revoke(id string, now time.Time) (agentToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := append([]agentToken{}, s.tokens...)
	for i := range next {
		if next[i].ID == id {
			if next[i].RevokedAt != nil {
				return next[i], nil
			}
			at := now.UTC()
			next[i].RevokedAt = &at
			if err := s.saveLocked(next); err != nil {
				return agentToken{}, err
			}
			s.tokens = next
			return next[i], nil
		}
	}
	return agentToken{}, fmt.Errorf("令牌不存在")
}

func (s *agentTokenStore) list(now time.Time) []agentTokenView {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []agentTokenView{}
	for _, t := range s.tokens {
		result = append(result, t.view(now))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result
}

var errTokenInvalid = fmt.Errorf("Agent 令牌无效")

// authenticate 只在内存中更新最近使用时间，避免每个只读请求写盘。
func (s *agentTokenStore) authenticate(plain string, remote net.IP, now time.Time) (agentToken, error) {
	if !strings.HasPrefix(plain, "sbm_") || len(plain) > 128 {
		return agentToken{}, errTokenInvalid
	}
	sum := sha256.Sum256([]byte(plain))
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tokens {
		t := &s.tokens[i]
		if t.Hash != hash {
			continue
		}
		switch t.status(now) {
		case "revoked":
			return agentToken{}, fmt.Errorf("Agent 令牌已吊销")
		case "expired":
			return agentToken{}, fmt.Errorf("Agent 令牌已过期，请在设置中重新创建")
		}
		if len(t.CIDRs) > 0 {
			allowed := false
			if addr, ok := netip.AddrFromSlice(remote); ok {
				for _, cidr := range t.CIDRs {
					if p, e := netip.ParsePrefix(cidr); e == nil && p.Contains(addr.Unmap()) {
						allowed = true
					}
				}
			}
			if !allowed {
				return agentToken{}, fmt.Errorf("来源地址不在该令牌允许的网段内")
			}
		}
		at := now.UTC()
		t.LastUsedAt = &at
		return *t, nil
	}
	return agentToken{}, errTokenInvalid
}

func agentActor(c *gin.Context) (string, string) {
	if v, ok := c.Get("agent_token"); ok {
		t := v.(agentToken)
		return "token:" + t.Name + "#" + t.ID, t.ID
	}
	return "admin", ""
}

// requireAgentToken 只认 Bearer，不认会话 Cookie；旧路由同样不认 Bearer。
func (s *Server) requireAgentToken(c *gin.Context) {
	if s.agentTokens == nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "Agent 令牌服务不可用，请检查数据目录中的 agent-tokens.json", "code": "tokens_unavailable"})
		return
	}
	header := c.GetHeader("Authorization")
	scheme, plain, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		c.AbortWithStatusJSON(401, gin.H{"error": "需要 Authorization: Bearer <Agent 令牌>", "code": "token_required"})
		return
	}
	host, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	token, err := s.agentTokens.authenticate(strings.TrimSpace(plain), net.ParseIP(host), time.Now())
	if err != nil {
		if s.auth != nil && s.auth.limited(host) {
			c.AbortWithStatusJSON(429, gin.H{"error": "认证失败过于频繁，请一分钟后重试", "code": "rate_limited"})
			return
		}
		status := 401
		if strings.Contains(err.Error(), "网段") {
			status = 403
		}
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error(), "code": "token_rejected"})
		return
	}
	c.Set("agent_token", token)
	c.Next()
}

func requireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := c.Get("agent_token")
		if !ok || !v.(agentToken).has(scope) {
			c.AbortWithStatusJSON(403, gin.H{"error": "令牌缺少 " + scope + " 权限", "code": "scope_denied", "required_scope": scope})
			return
		}
		c.Next()
	}
}

// ==================== 令牌管理（仅管理员会话） ====================

func (s *Server) listAgentTokens(c *gin.Context) {
	if s.agentTokens == nil {
		c.JSON(503, gin.H{"error": "Agent 令牌服务不可用"})
		return
	}
	c.JSON(200, gin.H{"data": s.agentTokens.list(time.Now()), "scopes": agentScopes, "max_days": maxAgentTokenDays})
}

func (s *Server) createAgentToken(c *gin.Context) {
	if s.agentTokens == nil {
		c.JSON(503, gin.H{"error": "Agent 令牌服务不可用"})
		return
	}
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
		CIDRs  []string `json:"cidrs"`
		Days   int      `json:"days"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "令牌参数无效"})
		return
	}
	token, plain, err := s.agentTokens.create(req.Name, req.Scopes, req.CIDRs, req.Days, time.Now())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	s.audit("admin", "token.create", token.Name+"#"+token.ID, "ok", strings.Join(token.Scopes, ","))
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"data": token.view(time.Now()), "token": plain, "message": "令牌只显示这一次，请立即保存"})
}

func (s *Server) revokeAgentToken(c *gin.Context) {
	if s.agentTokens == nil {
		c.JSON(503, gin.H{"error": "Agent 令牌服务不可用"})
		return
	}
	token, err := s.agentTokens.revoke(c.Param("id"), time.Now())
	if err != nil {
		c.JSON(404, gin.H{"error": err.Error()})
		return
	}
	s.audit("admin", "token.revoke", token.Name+"#"+token.ID, "ok", "")
	c.JSON(200, gin.H{"data": token.view(time.Now()), "message": "令牌已吊销"})
}
