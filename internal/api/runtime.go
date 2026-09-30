package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/runtimecontrol"
)

type runtimeProvider interface {
	WithRuntime(context.Context, func(daemon.RuntimeTarget) error) error
}

var errStaleRuntime = errors.New("内核实例已变化，请刷新页面后重试")

type proxySnapshot struct {
	Instance string                 `json:"instance"`
	Version  string                 `json:"version"`
	Proxies  []runtimecontrol.Proxy `json:"proxies"`
}
type connectionSnapshot struct {
	Instance  string `json:"instance"`
	SampledAt int64  `json:"sampled_at"`
	runtimecontrol.Connections
}
type closeResult struct {
	Closed  []string `json:"closed"`
	Missing []string `json:"missing"`
	Failed  []string `json:"failed"`
}

func (s *Server) setupRuntimeRoutes(api *gin.RouterGroup) {
	api.GET("/runtime/proxies", s.runtimeProxies)
	api.GET("/runtime/connections", s.runtimeConnections)
	api.POST("/runtime/select", s.runtimeSelect)
	api.POST("/runtime/delay", s.runtimeDelay)
	api.POST("/runtime/connections/close", s.runtimeClose)
}

func (s *Server) withRuntime(ctx context.Context, expected string, fn func(*runtimecontrol.Client, string) error) error {
	provider, ok := s.processManager.(runtimeProvider)
	if !ok {
		return daemon.ErrRuntimeStopped
	}
	return provider.WithRuntime(ctx, func(target daemon.RuntimeTarget) error {
		if expected != "" && expected != target.Instance {
			return errStaleRuntime
		}
		client, err := runtimecontrol.NewClient(target.Controller, target.Secret)
		if err != nil {
			return err
		}
		return fn(client, target.Instance)
	})
}

func runtimeError(c *gin.Context, err error) {
	status, message := http.StatusBadGateway, "内核控制 API 请求失败，请确认当前内核支持此功能"
	switch {
	case errors.Is(err, errStaleRuntime), errors.Is(err, daemon.ErrRuntimeChanged), errors.Is(err, runtimecontrol.ErrSelectionChanged):
		status, message = 409, "运行状态已变化，请刷新；无法确认配置时请通过服务页重启内核"
	case errors.Is(err, daemon.ErrRuntimeBusy), errors.Is(err, daemon.ErrRuntimeStopped), errors.Is(err, daemon.ErrRuntimeDisabled):
		status, message = 503, err.Error()
	case errors.Is(err, runtimecontrol.ErrInvalidTestURL):
		status, message = 400, err.Error()
	case errors.Is(err, runtimecontrol.ErrInvalidRequest):
		status, message = 400, "请求参数无效，请检查代理组、节点、连接 ID 或 HTTPS 测试地址"
	case errors.Is(err, runtimecontrol.ErrUnsupported):
		status, message = 501, "当前内核不支持此控制功能"
	case errors.Is(err, runtimecontrol.ErrUnauthorized):
		status, message = 502, "内核控制接口认证失败，请核对已应用配置"
	case errors.Is(err, runtimecontrol.ErrNotFound):
		status, message = 404, "代理组或连接已不存在，请刷新"
	case errors.Is(err, context.DeadlineExceeded):
		status, message = 504, "内核请求超时，请稍后重试"
	}
	c.JSON(status, gin.H{"error": message})
}

func readProxySnapshot(ctx context.Context, client *runtimecontrol.Client, instance string) (proxySnapshot, error) {
	version, err := client.Version(ctx)
	if err != nil {
		return proxySnapshot{}, err
	}
	proxies, err := client.Proxies(ctx)
	return proxySnapshot{Instance: instance, Version: version, Proxies: proxies}, err
}

func (s *Server) runtimeProxies(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()
	var result proxySnapshot
	err := s.withRuntime(ctx, "", func(client *runtimecontrol.Client, instance string) error {
		var err error
		result, err = readProxySnapshot(ctx, client, instance)
		return err
	})
	if err != nil {
		runtimeError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Server) runtimeConnections(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 6*time.Second)
	defer cancel()
	var result connectionSnapshot
	err := s.withRuntime(ctx, "", func(client *runtimecontrol.Client, instance string) error {
		snapshot, err := client.Connections(ctx)
		result = connectionSnapshot{Instance: instance, SampledAt: time.Now().UnixMilli(), Connections: snapshot}
		return err
	})
	if err != nil {
		runtimeError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Server) runtimeSelect(c *gin.Context) {
	var req struct {
		Group    string `json:"group"`
		Member   string `json:"member"`
		Instance string `json:"instance"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Instance == "" || req.Group == "" || req.Member == "" {
		c.JSON(400, gin.H{"error": "需要运行实例、代理组和目标节点"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if !s.runtimeWriteMu.TryLock() {
		runtimeError(c, daemon.ErrRuntimeBusy)
		return
	}
	defer s.runtimeWriteMu.Unlock()
	var result proxySnapshot
	err := s.withRuntime(ctx, req.Instance, func(client *runtimecontrol.Client, instance string) error {
		if err := client.Select(ctx, req.Group, req.Member); err != nil {
			return err
		}
		var err error
		result, err = readProxySnapshot(ctx, client, instance)
		return err
	})
	if err != nil {
		runtimeError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

func (s *Server) runtimeClose(c *gin.Context) {
	var req struct {
		IDs      []string `json:"ids"`
		Instance string   `json:"instance"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Instance == "" || len(req.IDs) == 0 || len(req.IDs) > 2000 {
		c.JSON(400, gin.H{"error": "每次需要 1–2000 个确认过的连接 ID 和运行实例"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if !s.runtimeWriteMu.TryLock() {
		runtimeError(c, daemon.ErrRuntimeBusy)
		return
	}
	defer s.runtimeWriteMu.Unlock()
	result := closeResult{Closed: []string{}, Missing: []string{}, Failed: []string{}}
	err := s.withRuntime(ctx, req.Instance, func(client *runtimecontrol.Client, _ string) error {
		current, err := client.Connections(ctx)
		if err != nil {
			return err
		}
		live := map[string]bool{}
		for _, connection := range current.Connections {
			live[connection.ID] = true
		}
		seen := map[string]bool{}
		for _, id := range req.IDs {
			if seen[id] {
				continue
			}
			seen[id] = true
			if !live[id] {
				result.Missing = append(result.Missing, id)
				continue
			}
			if err := client.Close(ctx, id); err != nil {
				result.Failed = append(result.Failed, id)
			} else {
				result.Closed = append(result.Closed, id)
			}
		}
		return nil
	})
	if err != nil {
		runtimeError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": result})
}

type delayResult struct {
	Tag   string `json:"tag"`
	Delay *int   `json:"delay"`
	Error string `json:"error,omitempty"`
}

func delayTargets(proxies []runtimecontrol.Proxy, tags []string) ([]string, error) {
	byTag := map[string]runtimecontrol.Proxy{}
	for _, p := range proxies {
		byTag[p.Tag] = p
	}
	seen := map[string]bool{}
	result := []string{}
	var visit func(string) error
	visit = func(tag string) error {
		if seen[tag] {
			return nil
		}
		seen[tag] = true
		p, ok := byTag[tag]
		if !ok {
			return runtimecontrol.ErrNotFound
		}
		if len(p.Members) > 0 {
			for _, child := range p.Members {
				if err := visit(child); err != nil {
					return err
				}
			}
		} else {
			result = append(result, tag)
		}
		if len(result) > 512 {
			return runtimecontrol.ErrInvalidRequest
		}
		return nil
	}
	for _, tag := range tags {
		if err := visit(tag); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Server) runtimeDelay(c *gin.Context) {
	var req struct {
		Tags     []string `json:"tags"`
		URL      string   `json:"url"`
		Timeout  int      `json:"timeout"`
		Instance string   `json:"instance"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Instance == "" || len(req.Tags) == 0 || len(req.Tags) > 512 || req.Timeout < 100 || req.Timeout > 10000 {
		c.JSON(400, gin.H{"error": "测速需要有效实例、节点列表和 100–10000ms 超时"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	var tags []string
	err := s.withRuntime(ctx, req.Instance, func(active *runtimecontrol.Client, _ string) error {
		proxies, err := active.Proxies(ctx)
		if err != nil {
			return err
		}
		tags, err = delayTargets(proxies, req.Tags)
		return err
	})
	if err != nil {
		runtimeError(c, err)
		return
	}
	results := make([]delayResult, len(tags))
	var workers sync.WaitGroup
	// 每个探测持有生命周期读锁，重启等待在途请求完成；排队请求须重验实例。
	// 全服务共用并发限额，避免多页面同时测速放大负载。
	jobs := make(chan int)
	for w := 0; w < 4; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				results[i].Tag = tags[i]
				select {
				case s.runtimeDelaySlots <- struct{}{}:
				case <-ctx.Done():
					results[i].Error = "测速超时或已取消"
					continue
				}
				var delay *int
				err := s.withRuntime(ctx, req.Instance, func(active *runtimecontrol.Client, _ string) error {
					var err error
					delay, err = active.Delay(ctx, tags[i], req.URL, req.Timeout)
					return err
				})
				<-s.runtimeDelaySlots
				if err != nil {
					switch {
					case errors.Is(err, daemon.ErrRuntimeBusy), errors.Is(err, errStaleRuntime), errors.Is(err, daemon.ErrRuntimeStopped):
						results[i].Error = "内核正在切换或已停止，本次测速未完成"
					case errors.Is(err, runtimecontrol.ErrInvalidTestURL):
						results[i].Error = err.Error()
					case errors.Is(err, runtimecontrol.ErrInvalidRequest):
						results[i].Error = "测试地址必须是有效的公网 HTTPS 地址"
					default:
						results[i].Error = "测速失败或目标不可达"
					}
				} else {
					results[i].Delay = delay
				}
			}
		}()
	}
	for i := range tags {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	err = s.withRuntime(c.Request.Context(), req.Instance, func(*runtimecontrol.Client, string) error { return nil })
	if err != nil {
		runtimeError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": gin.H{"results": results}})
}

// 保留编译期接口检查，生产环境不允许通过草案设置拼装控制目标。
var _ runtimeProvider = (*daemon.ProcessManager)(nil)
