package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xiaobei/singbox-manager/internal/logger"
	"github.com/xiaobei/singbox-manager/internal/parser"
	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 变更集状态。applying/pending_confirm/rolling_back/recovery_required 期间阻断其他配置写入。
const (
	csChecked    = "checked"
	csApplying   = "applying"
	csPending    = "pending_confirm"
	csRolling    = "rolling_back"
	csCommitted  = "committed"
	csRolledBack = "rolled_back"
	csFailed     = "failed"
	csRecovery   = "recovery_required"
	csExpired    = "expired"
	csResolved   = "resolved"

	defaultConfirmSeconds = 120
	minConfirmSeconds     = 30
	maxConfirmSeconds     = 600
	changesetTTL          = time.Hour
	maxChangesetFiles     = 200
	maxOpsPerChangeset    = 100
	// 已确认变更保留恢复数据的数量，用于「撤销刚才的修改」。
	maxRevertible = 20
)

type pendingDrift struct {
	Digest        string   `json:"digest"`
	Changes       []string `json:"changes"`
	AppliedExists bool     `json:"applied_exists"`
}

type changeset struct {
	ID              string        `json:"id"`
	Title           string        `json:"title,omitempty"`
	Actor           string        `json:"actor"`
	TokenID         string        `json:"token_id"`
	State           string        `json:"state"`
	Ops             []agentOp     `json:"ops"`
	BaseRev         string        `json:"base_rev"`
	CandidateRev    string        `json:"candidate_rev"`
	CandidateConfig string        `json:"candidate_config"`
	IncludePending  string        `json:"include_pending,omitempty"`
	Pending         *pendingDrift `json:"pending,omitempty"`
	Changes         []string      `json:"changes"`
	ConfigChanges   []string      `json:"config_changes"`
	Warnings        []string      `json:"warnings"`
	KernelChecked   bool          `json:"kernel_checked"`
	CreateKey       string        `json:"create_key,omitempty"`
	CreateDigest    string        `json:"create_digest,omitempty"`
	ApplyKey        string        `json:"apply_key,omitempty"`
	ConfirmSeconds  int           `json:"confirm_seconds,omitempty"`
	RuntimeBefore   *bool         `json:"runtime_before,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	ExpiresAt       time.Time     `json:"expires_at"`
	AppliedAt       *time.Time    `json:"applied_at,omitempty"`
	Deadline        *time.Time    `json:"deadline,omitempty"`
	FinishedAt      *time.Time    `json:"finished_at,omitempty"`
	Error           string        `json:"error,omitempty"`
	Resolution      string        `json:"resolution,omitempty"`
}

// recoveryPoint 在任何运行态写入前落盘，manager 重启后据此恢复。
// BootID 区分管理器重启与主机重启：跨启动时只恢复数据与停止态下的配置文件，不启动内核。
type recoveryPoint struct {
	Store         *storage.AppData `json:"store"`
	Config        []byte           `json:"config,omitempty"`
	ConfigExisted bool             `json:"config_existed"`
	Running       bool             `json:"running"`
	BootID        string           `json:"boot_id"`
}

type agentManager struct {
	mu     sync.Mutex
	dir    string
	sets   map[string]*changeset
	idem   map[string]string
	timers map[string]*time.Timer
	err    error
	// persistHook 仅供测试注入落盘失败。
	persistHook func(*changeset) error
}

type agentError struct {
	Status int
	Code   string
	Msg    string
	Extra  gin.H
}

func (e *agentError) Error() string { return e.Msg }

func agentFail(status int, code, format string, args ...any) *agentError {
	return &agentError{Status: status, Code: code, Msg: fmt.Sprintf(format, args...)}
}

func newAgentManager(dataDir string) *agentManager {
	m := &agentManager{dir: filepath.Join(dataDir, "agent", "changesets"), sets: map[string]*changeset{}, idem: map[string]string{}, timers: map[string]*time.Timer{}}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		m.err = err
		return m
	}
	_ = os.Chmod(filepath.Dir(m.dir), 0700)
	_ = os.Chmod(m.dir, 0700)
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		m.err = err
		return m
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".recovery.json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(m.dir, name))
		if err != nil {
			m.err = err
			continue
		}
		var cs changeset
		if json.Unmarshal(raw, &cs) != nil || cs.ID+".json" != name {
			// 无法解析的日志不能静默忽略，否则可能遗漏未确认操作。
			m.err = fmt.Errorf("变更日志 %s 损坏", name)
			continue
		}
		m.sets[cs.ID] = &cs
		if cs.CreateKey != "" {
			m.idem[cs.TokenID+"|"+cs.CreateKey] = cs.ID
		}
	}
	return m
}

func (m *agentManager) path(id string) string { return filepath.Join(m.dir, id+".json") }
func (m *agentManager) recoveryPath(id string) string {
	return filepath.Join(m.dir, id+".recovery.json")
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// persist 原子替换并同步父目录，崩溃后不会看到半写入的状态。
func (m *agentManager) persist(cs *changeset) error {
	if m.persistHook != nil {
		if err := m.persistHook(cs); err != nil {
			return err
		}
	}
	cs.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	if err := privateWrite(m.path(cs.ID), raw); err != nil {
		return err
	}
	return syncDir(m.dir)
}

func (m *agentManager) writeRecovery(id string, point recoveryPoint) error {
	raw, err := json.Marshal(point)
	if err != nil {
		return err
	}
	if err := privateWrite(m.recoveryPath(id), raw); err != nil {
		return err
	}
	return syncDir(m.dir)
}

func (m *agentManager) readRecovery(id string) (*recoveryPoint, error) {
	raw, err := os.ReadFile(m.recoveryPath(id))
	if err != nil {
		return nil, err
	}
	var point recoveryPoint
	if err := json.Unmarshal(raw, &point); err != nil || point.Store == nil || point.Store.Settings == nil {
		return nil, fmt.Errorf("恢复点损坏")
	}
	return &point, nil
}

func (m *agentManager) dropRecovery(id string) {
	_ = os.Remove(m.recoveryPath(id))
	_ = syncDir(m.dir)
}

// prune 先把过期未应用的变更集落为 expired，再只清理终态记录；活动与待恢复记录永不删除。
func (m *agentManager) pruneLocked() {
	now := time.Now()
	for _, cs := range m.sets {
		if cs.State == csChecked && now.After(cs.ExpiresAt) {
			cs.State = csExpired
			_ = m.persist(cs)
		}
	}
	terminal := []*changeset{}
	for _, cs := range m.sets {
		switch cs.State {
		case csCommitted, csRolledBack, csFailed, csExpired, csResolved:
			terminal = append(terminal, cs)
		}
	}
	// 为下一次创建留出一个位置。
	if len(m.sets) < maxChangesetFiles {
		return
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].CreatedAt.Before(terminal[j].CreatedAt) })
	for _, cs := range terminal {
		if len(m.sets) < maxChangesetFiles {
			break
		}
		_ = os.Remove(m.path(cs.ID))
		_ = os.Remove(m.recoveryPath(cs.ID))
		delete(m.sets, cs.ID)
		if cs.CreateKey != "" {
			delete(m.idem, cs.TokenID+"|"+cs.CreateKey)
		}
	}
}

// pruneRecoveryLocked 只为最近 maxRevertible 个已确认变更保留恢复数据。
func (m *agentManager) pruneRecoveryLocked() {
	committed := []*changeset{}
	for _, cs := range m.sets {
		if cs.State == csCommitted {
			committed = append(committed, cs)
		}
	}
	sort.Slice(committed, func(i, j int) bool { return committed[i].CreatedAt.After(committed[j].CreatedAt) })
	for i, cs := range committed {
		if i >= maxRevertible {
			_ = os.Remove(m.recoveryPath(cs.ID))
		}
	}
}

// settleLocked 先可靠落盘终态，再按需清理恢复点；落盘失败时保留恢复点，
// 内存状态置为 recovery_required 以继续阻断写入，并返回明确失败。
func (s *Server) settleLocked(cs *changeset, keepRecovery bool) error {
	if err := s.agent.persist(cs); err != nil {
		cs.State = csRecovery
		cs.Error = "变更日志写入失败，已保留恢复点并暂停写入，需要人工核对"
		_ = s.agent.persist(cs)
		return agentFail(500, "journal_write_failed", "%s", cs.Error)
	}
	if !keepRecovery {
		s.agent.dropRecovery(cs.ID)
	}
	return nil
}

func (s *Server) journalUnavailable() error {
	if s.agent != nil && s.agent.err != nil {
		return agentFail(503, "journal_unavailable", "Agent 变更日志无法读取（%s），为避免遗漏未确认操作已暂停变更", s.agent.err.Error())
	}
	return nil
}

// blockingLocked 返回阻断配置写入的操作。
func (m *agentManager) blockingLocked() *changeset {
	var found *changeset
	for _, cs := range m.sets {
		switch cs.State {
		case csApplying, csPending, csRolling, csRecovery:
			if found == nil || cs.CreatedAt.Before(found.CreatedAt) {
				found = cs
			}
		}
	}
	return found
}

func (s *Server) agentBlocking() *changeset {
	if s.agent == nil {
		return nil
	}
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	if cs := s.agent.blockingLocked(); cs != nil {
		copy := *cs
		return &copy
	}
	return nil
}

// agentWriteBlocked 供旧路由与调度器使用：确认窗口或待人工恢复期间拒绝配置/运行态写入。
func (s *Server) agentWriteBlocked() bool {
	if s.agent != nil && s.agent.err != nil {
		return true
	}
	return s.agentBlocking() != nil
}

func (s *Server) agentGateResponse(c *gin.Context) bool {
	if s.agent != nil && s.agent.err != nil {
		c.AbortWithStatusJSON(503, gin.H{"error": "Agent 变更日志无法读取，为避免覆盖未确认的恢复点已暂停配置写入：" + s.agent.err.Error(), "code": "journal_unavailable"})
		return true
	}
	cs := s.agentBlocking()
	if cs == nil {
		return false
	}
	msg := "Agent 变更正在等待确认，确认或回滚前暂停其他配置与运行态写入"
	if cs.State == csRecovery {
		msg = "Agent 变更恢复需要人工处理，处理前暂停其他配置与运行态写入"
	}
	c.AbortWithStatusJSON(409, gin.H{"error": msg, "code": "operation_pending", "operation": cs.ID, "state": cs.State, "deadline": cs.Deadline})
	return true
}

// ==================== 摘要与差异 ====================

func canonicalJSON(raw []byte) ([]byte, bool) {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	out, _ := json.Marshal(v)
	return out, true
}

func digestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// configDigest 按 JSON 语义比较，不受缩进与键序影响。
func configDigest(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if c, ok := canonicalJSON(raw); ok {
		return digestBytes(c)
	}
	return digestBytes(raw)
}

func storeDigest(data *storage.AppData) string {
	raw, _ := json.Marshal(data)
	return configDigest(raw)
}

func (s *Server) readAppliedConfig() ([]byte, bool, error) {
	raw, err := os.ReadFile(s.resolvePath(s.store.GetSettings().ConfigPath))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return raw, err == nil, err
}

// pendingDrift 比较当前存储生成的配置与已应用配置；差异即未应用草案。
func (s *Server) pendingDrift(data *storage.AppData) (*pendingDrift, error) {
	candidate, err := s.buildData(deepCopy(data), false)
	if err != nil {
		return nil, fmt.Errorf("当前已保存配置无法生成：%s", logger.Redact(err.Error()))
	}
	applied, exists, err := s.readAppliedConfig()
	if err != nil {
		return nil, fmt.Errorf("无法读取已应用配置")
	}
	a, b := configDigest(applied), configDigest([]byte(candidate))
	if a == b {
		return nil, nil
	}
	return &pendingDrift{Digest: digestBytes([]byte(a + "|" + b)), Changes: configChanges(applied, []byte(candidate)), AppliedExists: exists}, nil
}

var sensitiveValue = regexp.MustCompile(`://`)

func scalarText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		if len(x) > 80 || sensitiveValue.MatchString(x) {
			return "", false
		}
		return x, true
	case float64, bool:
		return fmt.Sprint(x), true
	case nil:
		return "null", true
	}
	return "", false
}

func taggedItems(v any) (map[string]any, []string, bool) {
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil, nil, false
	}
	items, order := map[string]any{}, []string{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		tag, _ := m["tag"].(string)
		if !ok || tag == "" {
			return nil, nil, false
		}
		items[tag] = item
		order = append(order, tag)
	}
	return items, order, true
}

func diffValue(path string, a, b any, depth int, out *[]string) {
	if reflect.DeepEqual(a, b) || len(*out) >= 60 {
		return
	}
	key := path[strings.LastIndex(path, ".")+1:]
	if logger.SensitiveKey(key) {
		*out = append(*out, path+" 已修改（敏感字段不显示）")
		return
	}
	if am, ok := a.(map[string]any); ok && depth > 0 {
		if bm, ok := b.(map[string]any); ok {
			keys := map[string]bool{}
			for k := range am {
				keys[k] = true
			}
			for k := range bm {
				keys[k] = true
			}
			sorted := []string{}
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				diffValue(strings.TrimPrefix(path+"."+k, "."), am[k], bm[k], depth-1, out)
			}
			return
		}
	}
	if ai, aOrder, ok := taggedItems(a); ok {
		if bi, bOrder, ok := taggedItems(b); ok {
			for _, tag := range bOrder {
				if _, exists := ai[tag]; !exists {
					*out = append(*out, fmt.Sprintf("%s 新增 %s", path, tag))
				} else if !reflect.DeepEqual(ai[tag], bi[tag]) {
					*out = append(*out, fmt.Sprintf("%s 修改 %s", path, tag))
				}
			}
			for _, tag := range aOrder {
				if _, exists := bi[tag]; !exists {
					*out = append(*out, fmt.Sprintf("%s 删除 %s", path, tag))
				}
			}
			return
		}
	}
	if al, ok := a.([]any); ok {
		if bl, ok := b.([]any); ok {
			changed := 0
			for i := 0; i < len(al) && i < len(bl); i++ {
				if !reflect.DeepEqual(al[i], bl[i]) {
					changed++
				}
			}
			*out = append(*out, fmt.Sprintf("%s：%d → %d 项，%d 项内容变化", path, len(al), len(bl), changed))
			return
		}
	}
	at, aok := scalarText(a)
	bt, bok := scalarText(b)
	if aok && bok {
		*out = append(*out, fmt.Sprintf("%s：%s → %s", path, at, bt))
		return
	}
	*out = append(*out, path+" 已修改")
}

// configChanges 只列出路径级差异，标量中不含 URL 与凭据。
func configChanges(oldRaw, newRaw []byte) []string {
	out := []string{}
	if len(oldRaw) == 0 {
		return []string{"尚无已应用配置，将首次生成运行配置"}
	}
	var a, b any
	if json.Unmarshal(oldRaw, &a) != nil {
		return []string{"已应用配置不是有效 JSON，将整体替换"}
	}
	_ = json.Unmarshal(newRaw, &b)
	diffValue("", a, b, 2, &out)
	if len(out) >= 60 {
		out = append(out, "……差异较多，已截断")
	}
	return out
}

func opSummary(op agentOp) string {
	label := op.ID
	var named struct {
		Name   string `json:"name"`
		Domain string `json:"domain"`
	}
	src := op.Value
	if len(op.Resolved) > 0 && op.Resource != "manual_node" {
		src = op.Resolved
	}
	_ = json.Unmarshal(src, &named)
	if named.Name != "" {
		label += "（" + named.Name + "）"
	} else if named.Domain != "" {
		label += "（" + named.Domain + "）"
	}
	switch {
	case op.Resource == "subscription" && len(op.Resolved) > 0:
		var sub storage.Subscription
		_ = json.Unmarshal(op.Resolved, &sub)
		return fmt.Sprintf("%s subscription %s：拉取到 %d 个节点", op.Action, label, len(sub.Nodes))
	case op.Resource == "manual_node" && len(op.Resolved) > 0:
		var node storage.ManualNode
		_ = json.Unmarshal(op.Resolved, &node)
		return fmt.Sprintf("create manual_node %s（%s %s）", op.ID, node.Node.Type, node.Node.Tag)
	case op.Resource == "blocklist":
		var req struct {
			Domains []string `json:"domains"`
		}
		_ = json.Unmarshal(op.Value, &req)
		return fmt.Sprintf("%s blocklist：%s", op.Action, strings.Join(req.Domains, ", "))
	case op.Resource == "revert":
		return "revert changeset " + op.ID + "：恢复到该变更应用前的状态"
	case op.Resource == "dns":
		keys := []string{}
		patch, _ := patchKeys(op.Value)
		for k := range patch {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "update dns：" + strings.Join(keys, ", ")
	}
	return strings.TrimSpace(op.Action + " " + op.Resource + " " + label)
}

// redactAny 用于返回、错误和审计：凭据字段与带 scheme 的 URL 一律隐藏。
func redactAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			lower := strings.ToLower(k)
			if logger.SensitiveKey(k) || lower == "url" || lower == "resolved" {
				if value != nil && value != "" {
					x[k] = "[REDACTED]"
				}
			} else {
				x[k] = redactAny(value)
			}
		}
	case []any:
		for i := range x {
			x[i] = redactAny(x[i])
		}
	case string:
		return logger.Redact(x)
	}
	return v
}

func redactedJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out any
	_ = json.Unmarshal(raw, &out)
	return redactAny(out)
}

// changesetView 不返回 Resolved 与原始值中的凭据。
func changesetView(cs *changeset) gin.H {
	ops := []any{}
	for _, op := range cs.Ops {
		var value any
		_ = json.Unmarshal(op.Value, &value)
		ops = append(ops, gin.H{"action": op.Action, "resource": op.Resource, "id": op.ID, "value": redactAny(value)})
	}
	view := gin.H{
		"id": cs.ID, "title": cs.Title, "actor": cs.Actor, "state": cs.State, "ops": ops,
		"base_rev": cs.BaseRev, "candidate_rev": cs.CandidateRev, "candidate_config": cs.CandidateConfig,
		"include_pending": cs.IncludePending, "pending": cs.Pending, "changes": cs.Changes, "config_changes": cs.ConfigChanges,
		"warnings": cs.Warnings, "kernel_checked": cs.KernelChecked, "confirm_seconds": cs.ConfirmSeconds,
		"runtime_before": cs.RuntimeBefore, "created_at": cs.CreatedAt, "updated_at": cs.UpdatedAt, "expires_at": cs.ExpiresAt,
		"applied_at": cs.AppliedAt, "deadline": cs.Deadline, "finished_at": cs.FinishedAt,
		"error": logger.Redact(cs.Error), "resolution": cs.Resolution,
	}
	if cs.State == csChecked && time.Now().After(cs.ExpiresAt) {
		view["state"], view["apply_ready"], view["blocked_reason"] = csExpired, false, "变更集已过期，请重新创建"
	} else if cs.State == csChecked {
		ready, reason := true, ""
		if cs.Pending != nil && cs.Pending.Digest != cs.IncludePending {
			ready, reason = false, "存在未应用的已保存改动；确认要一并应用时，用 include_pending="+cs.Pending.Digest+" 重新创建变更集"
		}
		view["apply_ready"], view["blocked_reason"] = ready, reason
	}
	return view
}

// ==================== 候选构建 ====================

func (s *Server) opEnv(data *storage.AppData) opEnv {
	return opEnv{gateway: data.Settings.DeploymentRole == "gateway", fetchSub: s.fetchSubscription, parseNode: parser.ParseURL, now: time.Now().UTC()}
}

// buildCandidate 在快照副本上重放操作并完成全部离线校验。
func (s *Server) buildCandidate(base *storage.AppData, ops []agentOp) (*storage.AppData, string, error) {
	cand := deepCopy(base)
	env := s.opEnv(cand)
	for i := range ops {
		if err := applyOp(cand, &ops[i], env); err != nil {
			msg := fmt.Sprintf("第 %d 项（%s %s）：%s", i+1, ops[i].Action, ops[i].Resource, err.Error())
			var ae *agentError
			if errors.As(err, &ae) {
				return nil, "", &agentError{Status: ae.Status, Code: ae.Code, Msg: msg}
			}
			return nil, "", fmt.Errorf("%s", msg)
		}
	}
	before, after := deepCopy(base.Settings), deepCopy(cand.Settings)
	storage.NormalizeSettings(before)
	storage.NormalizeSettings(after)
	// 设备启停/改址会改变 bypass 网段进而改变网关摘要；首版不执行任何网关变更。
	if before.DeploymentRole != after.DeploymentRole || !reflect.DeepEqual(before.Gateway, after.Gateway) {
		return nil, "", agentFail(409, "gateway_change_forbidden", "该变更会改变规范化的网关配置（例如 bypass 设备网段），Agent 首版不执行网关拓扑变更，请在网关页面由管理员操作")
	}
	if err := storage.ValidateSettings(after); err != nil {
		return nil, "", err
	}
	for _, r := range cand.Rules {
		if err := storage.ValidateRule(r, after.DeploymentRole == "gateway"); err != nil {
			return nil, "", fmt.Errorf("规则 %s：%s", r.ID, err.Error())
		}
	}
	raw, err := s.buildData(deepCopy(cand), false)
	if err != nil {
		return nil, "", fmt.Errorf("生成配置失败：%s", err.Error())
	}
	return cand, raw, nil
}

// ==================== 创建 ====================

type createChangesetRequest struct {
	Title          string    `json:"title"`
	Ops            []agentOp `json:"ops"`
	IncludePending string    `json:"include_pending"`
}

var idemPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func (s *Server) createChangeset(actor, tokenID, key string, body []byte) (*changeset, bool, error) {
	if key != "" && !idemPattern.MatchString(key) {
		return nil, false, agentFail(400, "bad_idempotency_key", "Idempotency-Key 需为 8–128 位字母、数字、_ 或 -")
	}
	if err := s.journalUnavailable(); err != nil {
		return nil, false, err
	}
	bodyDigest := digestBytes(body)
	if key != "" {
		if replay, err := s.replayCreate(tokenID, key, bodyDigest); replay != nil || err != nil {
			return replay, replay != nil, err
		}
	}
	var req createChangesetRequest
	if err := strictDecode(body, &req); err != nil {
		return nil, false, agentFail(400, "bad_request", "%s", err.Error())
	}
	if len(req.Ops) == 0 || len(req.Ops) > maxOpsPerChangeset {
		return nil, false, agentFail(400, "bad_request", "ops 需要 1–%d 项", maxOpsPerChangeset)
	}
	if len([]rune(req.Title)) > 120 {
		return nil, false, agentFail(400, "bad_request", "title 最长 120 个字符")
	}
	base := s.store.Snapshot()
	env := s.opEnv(base)
	ids := map[string]bool{}
	for i := range req.Ops {
		op := &req.Ops[i]
		if err := prepareOp(op, env); err != nil {
			return nil, false, agentFail(400, "invalid_op", "第 %d 项：%s", i+1, logger.Redact(err.Error()))
		}
		if op.Resource == "subscription" && op.Action == "refresh" {
			if err := prepareRefresh(op, base, env); err != nil {
				return nil, false, agentFail(400, "invalid_op", "第 %d 项：%s", i+1, logger.Redact(err.Error()))
			}
		}
		if (op.Resource == "revert" || op.Resource == "imported_rule") && len(req.Ops) != 1 {
			return nil, false, agentFail(400, "invalid_op", "%s 必须单独成一个变更集", op.Resource)
		}
		if op.Resource == "revert" {
			if err := s.prepareRevert(op, tokenID); err != nil {
				return nil, false, err
			}
		}
		if op.ID != "" {
			key := op.Resource + "/" + op.ID
			if ids[key] && op.Action == "create" {
				return nil, false, agentFail(400, "invalid_op", "第 %d 项：重复创建 %s", i+1, key)
			}
			ids[key] = true
		}
	}
	cand, raw, err := s.buildCandidate(base, req.Ops)
	if err != nil {
		var ae *agentError
		if errors.As(err, &ae) {
			return nil, false, ae
		}
		return nil, false, agentFail(400, "validation_failed", "%s", logger.Redact(err.Error()))
	}
	warnings := []string{}
	checked := false
	if _, verr := s.processManager.Version(); verr == nil {
		if err := s.processManager.CheckConfig([]byte(raw)); err != nil {
			return nil, false, agentFail(400, "kernel_check_failed", "内核校验失败：%s", logger.Redact(err.Error()))
		}
		checked = true
	} else {
		warnings = append(warnings, "未安装 sing-box 内核，只完成了离线校验；应用时仍会执行内核校验")
	}
	drift, err := s.pendingDrift(base)
	if err != nil {
		return nil, false, agentFail(409, "pending_unknown", "%s", err.Error())
	}
	if req.IncludePending != "" && (drift == nil || drift.Digest != req.IncludePending) {
		return nil, false, agentFail(409, "pending_mismatch", "include_pending 与当前未应用差异不一致，请重新查看 config diff")
	}
	if drift != nil && req.IncludePending == "" {
		warnings = append(warnings, "存在未应用的已保存改动，默认不会被一并应用；此变更集在差异消除前无法应用")
	}
	applied, _, _ := s.readAppliedConfig()
	changes := []string{}
	for _, op := range req.Ops {
		changes = append(changes, opSummary(op))
	}
	if s.store.GetSettings().DeploymentRole == "gateway" && !s.store.GetSettings().Gateway.Enabled {
		warnings = append(warnings, "网关角色尚未启用，此变更集无法应用；请先由管理员在网关页面启用")
	}
	now := time.Now().UTC()
	cs := &changeset{
		ID: uuid.New().String(), Title: strings.TrimSpace(req.Title), Actor: actor, TokenID: tokenID, State: csChecked, Ops: req.Ops,
		BaseRev: storeDigest(base), CandidateRev: storeDigest(cand), CandidateConfig: configDigest([]byte(raw)),
		IncludePending: req.IncludePending, Pending: drift, Changes: changes, ConfigChanges: configChanges(applied, []byte(raw)),
		Warnings: warnings, KernelChecked: checked, CreateKey: key, CreateDigest: bodyDigest,
		CreatedAt: now, ExpiresAt: now.Add(changesetTTL),
	}
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	if key != "" {
		if replay, err := s.replayCreateLocked(tokenID, key, bodyDigest); replay != nil || err != nil {
			return replay, replay != nil, err
		}
	}
	s.agent.pruneLocked()
	if len(s.agent.sets) >= maxChangesetFiles {
		return nil, false, agentFail(429, "too_many_changesets", "未应用的变更集过多（上限 %d），请等待过期或先应用/处理已有变更集", maxChangesetFiles)
	}
	if err := s.agent.persist(cs); err != nil {
		return nil, false, agentFail(500, "journal_write_failed", "保存变更集失败")
	}
	s.agent.sets[cs.ID] = cs
	if key != "" {
		s.agent.idem[tokenID+"|"+key] = cs.ID
	}
	s.agent.pruneLocked()
	copy := *cs
	return &copy, false, nil
}

// replayCreate 在锁内复制已有变更集，避免与状态修改并发读写。
func (s *Server) replayCreate(tokenID, key, digest string) (*changeset, error) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	return s.replayCreateLocked(tokenID, key, digest)
}

func (s *Server) replayCreateLocked(tokenID, key, digest string) (*changeset, error) {
	id, ok := s.agent.idem[tokenID+"|"+key]
	existing := s.agent.sets[id]
	if !ok || existing == nil {
		return nil, nil
	}
	if existing.CreateDigest != digest {
		return nil, agentFail(422, "idempotency_conflict", "同一 Idempotency-Key 已用于不同内容")
	}
	copy := *existing
	return &copy, nil
}

// ==================== 应用 ====================

// applyChangeset 调用方必须持有 writeMu；此处再取 operationMu 与内核操作串行。
func (s *Server) applyChangeset(id, actor, tokenID, key string, confirmSeconds int) (*changeset, bool, error) {
	if confirmSeconds == 0 {
		confirmSeconds = defaultConfirmSeconds
	}
	if confirmSeconds < minConfirmSeconds || confirmSeconds > maxConfirmSeconds {
		return nil, false, agentFail(400, "bad_request", "确认窗口需为 %d–%d 秒", minConfirmSeconds, maxConfirmSeconds)
	}
	if key != "" && !idemPattern.MatchString(key) {
		return nil, false, agentFail(400, "bad_idempotency_key", "Idempotency-Key 需为 8–128 位字母、数字、_ 或 -")
	}
	if err := s.journalUnavailable(); err != nil {
		return nil, false, err
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	s.agent.mu.Lock()
	cs := s.agent.sets[id]
	if cs == nil {
		s.agent.mu.Unlock()
		return nil, false, agentFail(404, "not_found", "变更集不存在")
	}
	if tokenID != "" && cs.TokenID != tokenID {
		s.agent.mu.Unlock()
		return nil, false, agentFail(403, "not_owner", "只能应用本令牌创建的变更集")
	}
	if cs.State != csChecked {
		// 同一请求超时重试时返回已记录的结果，不重复执行。
		if key != "" && key == cs.ApplyKey {
			if cs.ConfirmSeconds != confirmSeconds {
				s.agent.mu.Unlock()
				return nil, false, agentFail(422, "idempotency_conflict", "同一 Idempotency-Key 已用于不同的确认窗口")
			}
			copy := *cs
			s.agent.mu.Unlock()
			return &copy, true, nil
		}
		state := cs.State
		s.agent.mu.Unlock()
		return nil, false, agentFail(409, "already_"+state, "变更集当前状态为 %s，每个变更集只能应用一次", state)
	}
	if time.Now().After(cs.ExpiresAt) {
		cs.State = csExpired
		_ = s.agent.persist(cs)
		s.agent.mu.Unlock()
		return nil, false, agentFail(409, "expired", "变更集已过期，请重新创建")
	}
	if other := s.agent.blockingLocked(); other != nil {
		s.agent.mu.Unlock()
		return nil, false, &agentError{Status: 409, Code: "operation_pending", Msg: "已有变更等待确认或恢复", Extra: gin.H{"operation": other.ID, "state": other.State}}
	}
	ops := append([]agentOp{}, cs.Ops...)
	s.agent.mu.Unlock()

	current := s.store.Snapshot()
	if storeDigest(current) != cs.BaseRev {
		return nil, false, agentFail(409, "base_changed", "配置在创建变更集后已被修改，请重新创建变更集")
	}
	drift, err := s.pendingDrift(current)
	if err != nil {
		return nil, false, agentFail(409, "pending_unknown", "%s", err.Error())
	}
	if drift != nil && drift.Digest != cs.IncludePending {
		return nil, false, &agentError{Status: 409, Code: "pending_changes", Msg: "存在未应用的已保存改动，默认不夹带；确认需要一并应用时请用 include_pending 重新创建变更集", Extra: gin.H{"pending": drift}}
	}
	if drift == nil && cs.IncludePending != "" {
		return nil, false, agentFail(409, "pending_mismatch", "创建时的未应用差异已消失，请重新创建变更集")
	}
	cand, raw, err := s.buildCandidate(current, ops)
	if err != nil {
		var ae *agentError
		if errors.As(err, &ae) {
			return nil, false, ae
		}
		return nil, false, agentFail(400, "validation_failed", "%s", logger.Redact(err.Error()))
	}
	if storeDigest(cand) != cs.CandidateRev || configDigest([]byte(raw)) != cs.CandidateConfig {
		return nil, false, agentFail(409, "candidate_changed", "候选配置与预览不一致（可能内核版本变化），请重新创建变更集")
	}
	if cand.Settings.DeploymentRole == "gateway" {
		if err := s.requireGatewayState(cand.Settings); err != nil {
			return nil, false, agentFail(409, "gateway_not_ready", "%s", logger.Redact(err.Error()))
		}
	}
	applied, existed, err := s.readAppliedConfig()
	if err != nil {
		return nil, false, agentFail(500, "config_unreadable", "无法读取已应用配置")
	}
	running := s.processManager.IsRunning()
	if err := s.agent.writeRecovery(id, recoveryPoint{Store: current, Config: applied, ConfigExisted: existed, Running: running, BootID: s.bootID()}); err != nil {
		return nil, false, agentFail(500, "journal_write_failed", "写入恢复点失败，未做任何修改")
	}

	// applying 必须先于任何运行态写入持久化。
	now := time.Now().UTC()
	deadline := now.Add(time.Duration(confirmSeconds) * time.Second)
	s.agent.mu.Lock()
	cs.State, cs.ApplyKey, cs.ConfirmSeconds, cs.AppliedAt, cs.Deadline, cs.RuntimeBefore = csApplying, key, confirmSeconds, &now, &deadline, &running
	if err := s.agent.persist(cs); err != nil {
		cs.State, cs.ApplyKey, cs.AppliedAt, cs.Deadline, cs.RuntimeBefore = csChecked, "", nil, nil, nil
		s.agent.mu.Unlock()
		s.agent.dropRecovery(id)
		return nil, false, agentFail(500, "journal_write_failed", "写入变更日志失败，未做任何修改")
	}
	s.agent.mu.Unlock()

	fail := func(state, msg string) (*changeset, bool, error) {
		s.agent.mu.Lock()
		at := time.Now().UTC()
		cs.State, cs.Error, cs.FinishedAt, cs.Deadline = state, logger.Redact(msg), &at, nil
		settleErr := s.settleLocked(cs, state == csRecovery)
		copy := *cs
		s.agent.mu.Unlock()
		s.audit(actor, "changeset.apply", id, copy.State, msg)
		return &copy, false, settleErr
	}
	if err := s.store.Replace(cand); err != nil {
		return fail(csFailed, "保存候选配置失败，未修改运行配置")
	}
	if err := s.processManager.ApplyConfig([]byte(raw)); err != nil {
		msg := "应用失败：" + err.Error()
		if restoreErr := s.store.Replace(current); restoreErr != nil {
			return fail(csRecovery, msg+"；且无法恢复已保存配置")
		}
		after, _, _ := s.readAppliedConfig()
		if configDigest(after) != configDigest(applied) {
			return fail(csRecovery, msg+"；运行配置与恢复点不一致，需要人工核对")
		}
		if s.processManager.IsRunning() != running {
			return fail(csRecovery, msg+"；内核运行状态与应用前不一致，需要人工核对")
		}
		return fail(csFailed, msg+"；已恢复到应用前状态")
	}
	s.agent.mu.Lock()
	deadline = time.Now().UTC().Add(time.Duration(confirmSeconds) * time.Second)
	cs.State, cs.Deadline = csPending, &deadline
	persistErr := s.agent.persist(cs)
	s.scheduleTimeoutLocked(cs.ID, deadline)
	copy := *cs
	s.agent.mu.Unlock()
	if persistErr != nil {
		// 状态仍为 applying，重启时同样会恢复；运行中的计时器照常回滚。
		logger.Printf("Agent 变更日志写入失败，等待计时回滚")
	}
	s.audit(actor, "changeset.apply", id, csPending, fmt.Sprintf("确认窗口 %d 秒", confirmSeconds))
	return &copy, false, nil
}

func (s *Server) scheduleTimeoutLocked(id string, deadline time.Time) {
	if t := s.agent.timers[id]; t != nil {
		t.Stop()
	}
	s.agent.timers[id] = time.AfterFunc(time.Until(deadline), func() {
		s.writeMu.Lock()
		defer s.writeMu.Unlock()
		s.operationMu.Lock()
		defer s.operationMu.Unlock()
		// 确认已先完成时状态不再是待确认，CAS 失败属正常竞争结果。
		var ae *agentError
		if _, err := s.rollbackLocked(id, "system", "", "确认超时，已自动回滚"); err != nil && !(errors.As(err, &ae) && ae.Code == "not_pending") {
			logger.Printf("Agent 变更超时回滚未完成，需要人工处理")
		}
	})
}

// ==================== 确认与回滚 ====================

// confirmChangeset 调用方必须持有 writeMu；与超时回滚通过 writeMu + agent.mu 互斥。
func (s *Server) confirmChangeset(id, actor, tokenID, candidate string) (*changeset, bool, error) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	cs := s.agent.sets[id]
	if cs == nil {
		return nil, false, agentFail(404, "not_found", "变更集不存在")
	}
	if tokenID != "" && cs.TokenID != tokenID {
		return nil, false, agentFail(403, "not_owner", "只能确认本令牌应用的变更集")
	}
	if cs.State == csCommitted {
		copy := *cs
		return &copy, true, nil
	}
	if cs.State != csPending {
		if cs.State == csRolling {
			return nil, false, agentFail(409, "not_pending", "变更集正在回滚，无法确认")
		}
		return nil, false, agentFail(409, "not_pending", "变更集当前状态为 %s，无法确认", cs.State)
	}
	if candidate != cs.CandidateConfig {
		return nil, false, agentFail(409, "digest_mismatch", "确认需要提供本次应用的 candidate_config 摘要")
	}
	// 计时器可能因锁竞争稍晚触发；截止后一律不再接受确认。
	if cs.Deadline != nil && !time.Now().Before(*cs.Deadline) {
		return nil, false, agentFail(409, "deadline_passed", "确认窗口已截止，变更将被回滚")
	}
	applied, _, _ := s.readAppliedConfig()
	if storeDigest(s.store.Snapshot()) != cs.CandidateRev || configDigest(applied) != cs.CandidateConfig {
		cs.State, cs.Error = csRecovery, "确认时发现已保存或运行配置与本次应用不一致，已阻止后续覆盖，需要人工核对"
		_ = s.agent.persist(cs)
		s.stopTimerLocked(id)
		copy := *cs
		s.audit(actor, "changeset.confirm", id, csRecovery, cs.Error)
		return &copy, false, &agentError{Status: 409, Code: "recovery_required", Msg: cs.Error}
	}
	now := time.Now().UTC()
	deadline := cs.Deadline
	cs.State, cs.FinishedAt, cs.Deadline, cs.Resolution = csCommitted, &now, nil, "由 "+actor+" 确认"
	if err := s.agent.persist(cs); err != nil {
		cs.State, cs.FinishedAt, cs.Deadline, cs.Resolution = csPending, nil, deadline, ""
		return nil, false, agentFail(500, "journal_write_failed", "写入确认失败，仍处于待确认状态")
	}
	s.stopTimerLocked(id)
	// 保留恢复数据供后续撤销；只保留最近若干个。
	s.agent.pruneRecoveryLocked()
	copy := *cs
	s.audit(actor, "changeset.confirm", id, csCommitted, "")
	return &copy, false, nil
}

func (s *Server) stopTimerLocked(id string) {
	if t := s.agent.timers[id]; t != nil {
		t.Stop()
		delete(s.agent.timers, id)
	}
}

// rollbackLocked 调用方必须持有 writeMu 与 operationMu。
func (s *Server) rollbackLocked(id, actor, tokenID, reason string) (*changeset, error) {
	s.agent.mu.Lock()
	cs := s.agent.sets[id]
	if cs == nil {
		s.agent.mu.Unlock()
		return nil, agentFail(404, "not_found", "变更集不存在")
	}
	if tokenID != "" && cs.TokenID != tokenID {
		s.agent.mu.Unlock()
		return nil, agentFail(403, "not_owner", "只能回滚本令牌应用的变更集")
	}
	if cs.State == csRolledBack {
		copy := *cs
		s.agent.mu.Unlock()
		return &copy, nil
	}
	if cs.State != csPending && cs.State != csApplying && cs.State != csRolling {
		state := cs.State
		s.agent.mu.Unlock()
		return nil, agentFail(409, "not_pending", "变更集当前状态为 %s，无法回滚", state)
	}
	s.stopTimerLocked(id)
	candidateRev, candidateConfig := cs.CandidateRev, cs.CandidateConfig
	// 恢复内核可能需要数秒；先持久化 rolling_back，期间不可确认，重启后继续恢复。
	cs.State = csRolling
	if err := s.agent.persist(cs); err != nil {
		logger.Printf("Agent 变更回滚状态写入失败，继续回滚")
	}
	s.agent.mu.Unlock()

	note := ""
	finish := func(state, msg string) (*changeset, error) {
		s.agent.mu.Lock()
		defer s.agent.mu.Unlock()
		at := time.Now().UTC()
		cs.State, cs.FinishedAt, cs.Deadline = state, &at, nil
		if state == csRolledBack {
			cs.Resolution = reason + note + "（" + actor + "）"
		} else {
			cs.Error = logger.Redact(msg)
		}
		if err := s.settleLocked(cs, state != csRolledBack); err != nil {
			copy := *cs
			s.audit(actor, "changeset.rollback", id, cs.State, cs.Error)
			return &copy, err
		}
		copy := *cs
		s.audit(actor, "changeset.rollback", id, state, reason+note+" "+msg)
		if state == csRecovery {
			return &copy, &agentError{Status: 409, Code: "recovery_required", Msg: cs.Error}
		}
		return &copy, nil
	}
	point, err := s.agent.readRecovery(id)
	if err != nil {
		return finish(csRecovery, "恢复点不可用："+err.Error())
	}
	current := storeDigest(s.store.Snapshot())
	original := storeDigest(point.Store)
	// 只覆盖本次写入的结果，发现后续数据时保留现场等待人工处理。
	if current != candidateRev && current != original {
		return finish(csRecovery, "已保存配置在应用后又被修改，未自动覆盖，需要人工核对")
	}
	applied, _, _ := s.readAppliedConfig()
	// 运行配置也必须仍是本次候选或应用前原版，否则说明有其他途径改过，保留现场。
	if d := configDigest(applied); d != candidateConfig && d != configDigest(point.Config) {
		return finish(csRecovery, "运行配置在应用后被其他途径修改，未自动覆盖，需要人工核对")
	}
	currentBoot := s.bootID()
	crossBoot := point.BootID == "" || currentBoot == "" || point.BootID != currentBoot
	configDiffers := (point.ConfigExisted && configDigest(applied) != configDigest(point.Config)) || (!point.ConfigExisted && len(applied) > 0)
	if crossBoot && configDiffers && s.processManager.IsRunning() {
		return finish(csRecovery, "主机已重启且内核正在运行，未自动替换运行配置或重启内核，需要人工核对")
	}
	// 先验证运行配置、启动身份与网关条件，再修改草案，冲突时完整保留现场。
	if !crossBoot && point.Store.Settings.DeploymentRole == "gateway" {
		if err := s.requireGatewayState(point.Store.Settings); err != nil {
			return finish(csRecovery, "网关状态无法确认，未自动恢复运行配置："+err.Error())
		}
	}
	if current != original {
		if err := s.store.Replace(point.Store); err != nil {
			return finish(csRecovery, "恢复已保存配置失败")
		}
	}
	if crossBoot {
		// 主机已重启：不启动内核、不重启正在运行的内核；只在内核停止时恢复配置文件。
		if configDiffers && point.ConfigExisted {
			if err := s.processManager.ApplyConfig(point.Config); err != nil {
				return finish(csRecovery, "恢复运行配置失败："+err.Error())
			}
		} else if configDiffers {
			if err := os.Remove(s.resolvePath(point.Store.Settings.ConfigPath)); err != nil && !os.IsNotExist(err) {
				return finish(csRecovery, "删除新生成的运行配置失败")
			}
		}
		if point.Running {
			note = "；主机已重启，按启动边界未自动启动内核"
		}
		return finish(csRolledBack, "")
	}
	if point.ConfigExisted {
		if configDigest(applied) != configDigest(point.Config) {
			if err := s.processManager.ApplyConfig(point.Config); err != nil {
				return finish(csRecovery, "恢复运行配置失败："+err.Error())
			}
		}
	} else if len(applied) > 0 {
		if s.processManager.IsRunning() {
			if err := s.processManager.Stop(); err != nil {
				return finish(csRecovery, "停止内核失败："+err.Error())
			}
		}
		if err := os.Remove(s.resolvePath(point.Store.Settings.ConfigPath)); err != nil && !os.IsNotExist(err) {
			return finish(csRecovery, "删除新生成的运行配置失败")
		}
	}
	running := s.processManager.IsRunning()
	switch {
	case point.Running && !running:
		// 网关模式下只在辅助服务仍是同一份已应用网关时才恢复启动。
		if err := s.requireGatewayState(point.Store.Settings); err != nil {
			return finish(csRecovery, "网关状态无法确认，未自动启动内核："+err.Error())
		}
		if err := s.processManager.Start(); err != nil {
			return finish(csRecovery, "内核恢复启动失败："+err.Error())
		}
	case !point.Running && running:
		if err := s.processManager.Stop(); err != nil {
			return finish(csRecovery, "内核恢复停止状态失败："+err.Error())
		}
	}
	return finish(csRolledBack, "")
}

// RecoverAgentOperations 必须在开放 API 与启动调度前执行：任何未确认操作都优先回滚。
func (s *Server) RecoverAgentOperations() {
	if s.agent == nil {
		return
	}
	s.agent.mu.Lock()
	ids := []string{}
	for id, cs := range s.agent.sets {
		if cs.State == csApplying || cs.State == csPending || cs.State == csRolling {
			ids = append(ids, id)
		}
	}
	s.agent.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		s.writeMu.Lock()
		s.operationMu.Lock()
		if _, err := s.rollbackLocked(id, "system", "", "管理器启动时发现未确认的变更，已恢复"); err != nil {
			logger.Printf("启动恢复 Agent 变更未完成，需要人工处理")
		} else {
			logger.Printf("启动时已恢复未确认的 Agent 变更")
		}
		s.operationMu.Unlock()
		s.writeMu.Unlock()
	}
}

// resolveChangeset 由管理员在人工核对后解除 recovery_required 门禁。
func (s *Server) resolveChangeset(id, note string) (*changeset, error) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	cs := s.agent.sets[id]
	if cs == nil {
		return nil, agentFail(404, "not_found", "变更集不存在")
	}
	if cs.State != csRecovery {
		return nil, agentFail(409, "not_recovery", "只有待人工处理的变更可以标记为已处理")
	}
	now := time.Now().UTC()
	cs.State, cs.FinishedAt, cs.Resolution = csResolved, &now, "管理员已人工处理："+strings.TrimSpace(note)
	if err := s.agent.persist(cs); err != nil {
		cs.State = csRecovery
		return nil, agentFail(500, "journal_write_failed", "写入失败")
	}
	s.agent.dropRecovery(id)
	copy := *cs
	s.audit("admin", "changeset.resolve", id, csResolved, note)
	return &copy, nil
}

func (s *Server) listChangesets(limit int) []gin.H {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	list := []*changeset{}
	for _, cs := range s.agent.sets {
		list = append(list, cs)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	views := []gin.H{}
	for _, cs := range list {
		views = append(views, changesetView(cs))
	}
	return views
}

func (s *Server) getChangesetView(id string) (gin.H, bool) {
	s.agent.mu.Lock()
	defer s.agent.mu.Unlock()
	cs := s.agent.sets[id]
	if cs == nil {
		return nil, false
	}
	return changesetView(cs), true
}

type revertPayload struct {
	Target    string           `json:"target"`
	ExpectRev string           `json:"expect_rev"`
	Store     *storage.AppData `json:"store"`
}

// prepareRevert 把已确认变更的恢复数据固定到新变更集中；撤销本身也要预览、应用、确认。
func (s *Server) prepareRevert(op *agentOp, tokenID string) error {
	s.agent.mu.Lock()
	target := s.agent.sets[op.ID]
	var state, owner, rev string
	if target != nil {
		state, owner, rev = target.State, target.TokenID, target.CandidateRev
	}
	s.agent.mu.Unlock()
	if target == nil {
		return agentFail(404, "not_found", "要撤销的变更集不存在")
	}
	if tokenID != "" && owner != tokenID {
		return agentFail(403, "not_owner", "只能撤销本令牌创建的变更")
	}
	if state != csCommitted {
		return agentFail(409, "not_committed", "只能撤销已确认的变更（当前 %s）；未确认的变更请直接回滚", state)
	}
	point, err := s.agent.readRecovery(op.ID)
	if err != nil {
		return agentFail(409, "revert_unavailable", "该变更的恢复数据已清理（只保留最近 %d 个），无法自动撤销", maxRevertible)
	}
	op.Resolved, _ = json.Marshal(revertPayload{Target: op.ID, ExpectRev: rev, Store: point.Store})
	return nil
}
