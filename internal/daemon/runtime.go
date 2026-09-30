package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

var (
	ErrRuntimeBusy     = errors.New("内核正在启动或应用配置，请稍后刷新")
	ErrRuntimeStopped  = errors.New("受管内核未运行")
	ErrRuntimeChanged  = errors.New("无法确认运行配置，配置已变化或旧实例尚无启动摘要；请通过服务页重启后刷新")
	ErrRuntimeDisabled = errors.New("运行配置未启用控制 API，请在设置中配置端口并应用")
)

// RuntimeTarget 仅交给本机后端使用，不能序列化返回浏览器。
type RuntimeTarget struct{ Instance, Controller, Secret string }

// WithRuntime 与启动、停止、配置应用共用生命周期锁。浏览器携带的实例令牌
// 必须在此回调内比较；写操作和内核响应结束前不允许应用替换实例。
func (pm *ProcessManager) WithRuntime(ctx context.Context, fn func(RuntimeTarget) error) error {
	if !pm.opMu.TryRLock() {
		return ErrRuntimeBusy
	}
	defer pm.opMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	id := pm.currentIdentity()
	if !pm.matches(id) {
		return ErrRuntimeStopped
	}
	if id.ConfigHash == "" {
		return ErrRuntimeChanged
	}
	raw, err := os.ReadFile(id.Config)
	if err != nil {
		return ErrRuntimeChanged
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != id.ConfigHash {
		return ErrRuntimeChanged
	}
	var config struct {
		Experimental struct {
			Clash struct {
				Controller string `json:"external_controller"`
				Secret     string `json:"secret"`
			} `json:"clash_api"`
		} `json:"experimental"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return ErrRuntimeChanged
	}
	if config.Experimental.Clash.Controller == "" {
		return ErrRuntimeDisabled
	}
	token := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s", id.PID, id.Created, id.Config, id.ConfigHash)))
	err = fn(RuntimeTarget{Instance: hex.EncodeToString(token[:]), Controller: config.Experimental.Clash.Controller, Secret: config.Experimental.Clash.Secret})
	if err == nil && !pm.matches(id) {
		return ErrRuntimeChanged
	}
	return err
}
