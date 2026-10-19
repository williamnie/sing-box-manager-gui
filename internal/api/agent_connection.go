package api

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// managementTLS 从同一份证书生成 TLS 配置和接入指纹，避免文件替换后指纹与监听证书不一致。
func managementTLS(certFile, keyFile string) (*tls.Config, string, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(certificate.Certificate[0])
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}, hex.EncodeToString(sum[:]), nil
}

// 仅向登录管理员提供公开的连接参数；地址采用当前请求的真实 Host，不信任转发头。
func (s *Server) agentConnection(c *gin.Context) {
	host, port, err := net.SplitHostPort(c.Request.Host)
	if err != nil {
		host = strings.Trim(c.Request.Host, "[]")
		port = ""
	}
	ip := net.ParseIP(host)
	if host != "localhost" && ip == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法识别当前管理地址"})
		return
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "管理端口无效"})
			return
		}
	}
	scheme, fingerprint := "http", ""
	if c.Request.TLS != nil {
		scheme, fingerprint = "https", s.managementFingerprint
		if fingerprint == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "当前 HTTPS 证书信息不可用，无法生成接入命令"})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"server":              scheme + "://" + c.Request.Host,
		"fingerprint":         fingerprint,
		"allow_insecure_http": scheme == "http" && host != "localhost" && !ip.IsLoopback(),
	}})
}
