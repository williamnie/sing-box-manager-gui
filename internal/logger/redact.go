package logger

import (
	"regexp"
	"strings"
)

var sensitiveURL = regexp.MustCompile(`(?i)(https?|ss|ssr|vmess|vless|trojan|hysteria2|tuic|socks5?)://[^\s"<>]+`)
var sensitivePair = regexp.MustCompile(`(?i)(password|secret|token|uuid|private_key|authorization)(["'\s:=]+)[^\s,}"']+`)
var authorizationScheme = regexp.MustCompile(`(?i)((?:proxy-)?authorization["'\s:=]+)(?:bearer|basic)\s+[^\s,"'\]}]+`)
var terminalColor = regexp.MustCompile(`(?:\x1b|\[ESC\])\[[0-?]*[ -/]*[@-~]`)

// Redact 避免网络错误中的完整订阅 URL 或日志中的凭据进入控制台和磁盘。
func Redact(line string) string {
	// 兼容旧落盘日志的 [ESC] 标记，移除颜色控制码使级别过滤保持准确。
	line = terminalColor.ReplaceAllString(line, "")
	line = sensitiveURL.ReplaceAllString(line, "[URL REDACTED]")
	line = authorizationScheme.ReplaceAllString(line, "$1[REDACTED]")
	line = sensitivePair.ReplaceAllString(line, "$1$2[REDACTED]")
	return strings.ReplaceAll(line, "\x1b", "[ESC]")
}

// SensitiveKey 供配置预览及内核输出使用相同的凭据字段识别规则。
func SensitiveKey(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "password") || strings.Contains(k, "secret") || strings.Contains(k, "private_key") || strings.Contains(k, "token") || strings.HasPrefix(k, "auth") || k == "uuid" || k == "username" || k == "psk" || k == "pre_shared_key" || k == "client_key" || k == "key"
}
