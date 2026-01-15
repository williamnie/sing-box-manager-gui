package utils

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SubscriptionInfo 订阅信息（从响应头解析）
type SubscriptionInfo struct {
	Upload      int64      // 上传流量
	Download    int64      // 下载流量
	Total       int64      // 总流量
	Expire      *time.Time // 过期时间
	ContentType string     // 内容类型
}

// FetchSubscription 拉取订阅内容
func FetchSubscription(url string) (string, *SubscriptionInfo, error) {
	client := GetHTTPClient()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置 User-Agent
	req.Header.Set("User-Agent", "clash-verge/v1.0.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP 状态码: %d", resp.StatusCode)
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析订阅信息
	info := parseSubscriptionInfo(resp.Header)

	return string(body), info, nil
}

// parseSubscriptionInfo 从响应头解析订阅信息
func parseSubscriptionInfo(header http.Header) *SubscriptionInfo {
	info := &SubscriptionInfo{
		ContentType: header.Get("Content-Type"),
	}

	// 解析 subscription-userinfo 头
	// 格式: upload=xxx; download=xxx; total=xxx; expire=xxx
	userInfo := header.Get("subscription-userinfo")
	if userInfo == "" {
		userInfo = header.Get("Subscription-Userinfo")
	}

	if userInfo != "" {
		parts := strings.Split(userInfo, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])

			switch key {
			case "upload":
				info.Upload, _ = strconv.ParseInt(value, 10, 64)
			case "download":
				info.Download, _ = strconv.ParseInt(value, 10, 64)
			case "total":
				info.Total, _ = strconv.ParseInt(value, 10, 64)
			case "expire":
				if ts, err := strconv.ParseInt(value, 10, 64); err == nil {
					t := time.Unix(ts, 0)
					info.Expire = &t
				}
			}
		}
	}

	return info
}

// FormatBytes 格式化字节数
func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
