package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaobei/singbox-manager/internal/daemon"
	"github.com/xiaobei/singbox-manager/internal/dnsquery"
)

var dnsQueryType = regexp.MustCompile(`^[A-Z0-9]{1,16}$`)

func dnsQueryFilter(c *gin.Context, export bool) (dnsquery.Filter, bool) {
	f := dnsquery.Filter{Source: c.Query("source"), Search: strings.TrimSpace(c.Query("search")), Type: c.Query("type"), Limit: 50}
	bad := func() (dnsquery.Filter, bool) {
		c.JSON(400, gin.H{"error": "查询筛选无效，请检查设备、时间范围和分页"})
		return f, false
	}
	if len(f.Search) > 253 || f.Type != "" && !dnsQueryType.MatchString(f.Type) {
		return bad()
	}
	if sort := c.Query("sort"); sort != "" && sort != "recent" && sort != "count" {
		return bad()
	} else {
		f.Sort = sort
	}
	if f.Source != "" {
		addr, err := netip.ParseAddr(f.Source)
		if err != nil {
			return bad()
		}
		f.Source = addr.Unmap().String()
	}
	for key, value := range map[string]*int64{"since": &f.Since, "until": &f.Until} {
		if raw := c.Query(key); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				return bad()
			}
			*value = parsed
		}
	}
	if f.Until > 0 && f.Since > f.Until {
		return bad()
	}
	if export {
		f.Limit = 10000
		return f, true
	}
	for key, value := range map[string]*int{"offset": &f.Offset, "limit": &f.Limit} {
		if raw := c.Query(key); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 || key == "limit" && (parsed < 1 || parsed > 100) || key == "offset" && parsed > dnsquery.MaxRecords {
				return bad()
			}
			*value = parsed
		}
	}
	return f, true
}

func (s *Server) scanDNSQueries() {
	_ = s.dnsQueries.Scan(filepath.Join(s.store.GetDataDir(), "logs", "singbox.log"), s.store.GetSettings().DNSQueryLogEnabled, time.Now())
}

func (s *Server) dnsQueryStatus(ctx context.Context) gin.H {
	settings := s.store.GetSettings()
	stats := s.dnsQueries.Status()
	var config struct {
		Log struct {
			Level string `json:"level"`
		} `json:"log"`
		Inbounds []struct {
			Tag string `json:"tag"`
		} `json:"inbounds"`
	}
	applied := false
	lanDNS := false
	if raw, err := os.ReadFile(s.resolvePath(settings.ConfigPath)); err == nil && json.Unmarshal(raw, &config) == nil {
		for _, in := range config.Inbounds {
			lanDNS = lanDNS || in.Tag == "lan-dns"
		}
		if provider, ok := s.processManager.(runtimeProvider); ok {
			err = provider.WithRuntime(ctx, func(daemon.RuntimeTarget) error { return nil })
			applied = (err == nil || errors.Is(err, daemon.ErrRuntimeDisabled)) && lanDNS && (config.Log.Level == "debug" || config.Log.Level == "trace")
		}
	}
	return gin.H{"enabled": settings.DNSQueryLogEnabled, "applied": applied, "auto_apply": settings.AutoApply, "supported": settings.DeploymentRole == "gateway" || lanDNS, "stats": stats, "retention_days": dnsquery.RetentionDays, "max_records": dnsquery.MaxRecords, "max_bytes": dnsquery.MaxBytes}
}

func (s *Server) getDNSQueries(c *gin.Context) {
	f, ok := dnsQueryFilter(c, false)
	if !ok {
		return
	}
	s.scanDNSQueries()
	c.Header("Cache-Control", "no-store")
	result := s.dnsQueries.Search(f, time.Now())
	domains := make([]string, 0, len(result.Domains)+len(result.Records))
	for _, row := range result.Domains {
		domains = append(domains, row.Domain)
	}
	for _, row := range result.Records {
		domains = append(domains, row.Domain)
	}
	c.JSON(200, gin.H{"data": result, "status": s.dnsQueryStatus(c.Request.Context()), "classification": s.dnsDomainCheck.Check(domains)})
}

func (s *Server) refreshDNSDomainList(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if err := s.dnsDomainCheck.Refresh(c.Request.Context()); err != nil {
		c.JSON(502, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"status": s.dnsDomainCheck.Check(nil).Status})
}

func (s *Server) updateDNSQuerySettings(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&req) != nil || req.Enabled == nil {
		c.JSON(400, gin.H{"error": "请明确是否开启 DNS 查询记录"})
		return
	}
	settings := s.store.GetSettings()
	// 先提交旧开关下的日志，避免重新开启时把暂停期间的查询写入历史。
	s.scanDNSQueries()
	settings.DNSQueryLogEnabled = *req.Enabled
	if err := s.store.UpdateSettings(settings); err != nil {
		c.JSON(500, gin.H{"error": "保存 DNS 采集设置失败"})
		return
	}
	warning := ""
	if err := s.autoApplyConfig(); err != nil {
		warning = "设置已保存，但配置应用失败，请到配置审阅检查并应用：" + err.Error()
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"status": s.dnsQueryStatus(c.Request.Context()), "warning": warning})
}

func (s *Server) exportDNSQueries(c *gin.Context) {
	f, ok := dnsQueryFilter(c, true)
	if !ok {
		return
	}
	s.scanDNSQueries()
	result := s.dnsQueries.Search(f, time.Now())
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="dns-queries-`+time.Now().Format("20060102-150405")+`.csv"`)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("X-DNS-Export-Limit", "10000")
	c.Header("X-DNS-Filtered-Total", strconv.Itoa(result.Total))
	_, _ = c.Writer.Write([]byte{0xef, 0xbb, 0xbf})
	w := csv.NewWriter(c.Writer)
	_ = w.Write([]string{"时间", "来源设备", "域名", "查询类型"})
	for _, r := range result.Records {
		_ = w.Write([]string{time.UnixMilli(r.At).UTC().Format(time.RFC3339), r.Source, dnsquery.SafeCSV(r.Domain), r.Type})
	}
	w.Flush()
}
