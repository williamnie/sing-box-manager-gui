package api

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// managementTLS 从同一份证书生成 TLS 配置、接入指纹和 curl 公钥固定值，避免文件替换后与监听证书不一致。
func managementTLS(certFile, keyFile string) (*tls.Config, string, string, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, "", "", err
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(certificate.Certificate[0])
	spki := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	config := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	return config, hex.EncodeToString(sum[:]), "sha256//" + base64.StdEncoding.EncodeToString(spki[:]), nil
}

// agentEndpoint 描述 Agent 电脑连接当前服务所需的公开参数，均不含密钥。
type agentEndpoint struct {
	Server         string   `json:"server"`
	Fingerprint    string   `json:"fingerprint"`
	PinnedPubkey   string   `json:"pinned_pubkey"`
	AllowHTTP      bool     `json:"allow_insecure_http"`
	LocalOnly      bool     `json:"local_only"`
	SuggestedCIDRs []string `json:"suggested_cidrs"`
}

// 地址采用当前请求的真实 Host，不信任转发头。
func (s *Server) requestEndpoint(r *http.Request) (agentEndpoint, int, error) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = strings.Trim(r.Host, "[]")
		port = ""
	}
	ip := net.ParseIP(host)
	if host != "localhost" && ip == nil {
		return agentEndpoint{}, http.StatusBadRequest, fmt.Errorf("无法识别当前管理地址")
	}
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return agentEndpoint{}, http.StatusBadRequest, fmt.Errorf("管理端口无效")
		}
	}
	local := host == "localhost" || ip.IsLoopback()
	endpoint := agentEndpoint{Server: "http://" + r.Host, LocalOnly: local, SuggestedCIDRs: []string{}}
	if r.TLS != nil {
		if s.managementFingerprint == "" || s.managementPubkeyPin == "" {
			return agentEndpoint{}, http.StatusServiceUnavailable, fmt.Errorf("当前 HTTPS 证书信息不可用，无法生成接入命令")
		}
		endpoint.Server, endpoint.Fingerprint, endpoint.PinnedPubkey = "https://"+r.Host, s.managementFingerprint, s.managementPubkeyPin
	} else {
		endpoint.AllowHTTP = !local
	}
	// 管理器已限定来源网段时，Agent 电脑必然在其中，用作令牌来源网段的默认值。
	if !local {
		for _, network := range s.allowedNetworks {
			endpoint.SuggestedCIDRs = append(endpoint.SuggestedCIDRs, network.String())
		}
	}
	return endpoint, http.StatusOK, nil
}

// 仅向登录管理员提供公开的连接参数。
func (s *Server) agentConnection(c *gin.Context) {
	endpoint, status, err := s.requestEndpoint(c.Request)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": endpoint})
}

// shellQuote 生成单引号参数，地址中的任何字符都不能成为 shell 指令。
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// agentSetupScript 在 Agent 电脑上执行：从本管理器下载同版本 sbm、安装 Skill，再交互式连接。
// 脚本只含公开参数，令牌在终端中输入，不经过 URL 或命令行。
func (s *Server) agentSetupScript(c *gin.Context) {
	endpoint, status, err := s.requestEndpoint(c.Request)
	if err != nil {
		c.String(status, "echo %s >&2; exit 1\n", shellQuote(err.Error()))
		return
	}
	curl := "curl -sSL"
	connect := "--server " + shellQuote(endpoint.Server)
	if endpoint.PinnedPubkey != "" {
		// 自签证书不经 CA 校验，改由公钥固定保证连接的是这台管理器。
		curl = "curl -sSL --insecure --pinnedpubkey " + shellQuote(endpoint.PinnedPubkey)
		connect += " --fingerprint " + shellQuote(endpoint.Fingerprint)
	}
	if endpoint.AllowHTTP {
		connect += " --allow-insecure-http"
	}
	script := strings.NewReplacer("@SERVER@", shellQuote(endpoint.Server), "@CURL@", curl, "@CONNECT@", connect, "@VERSION@", shellQuote(s.version)).Replace(agentSetupTemplate)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(script))
}

const agentSetupTemplate = `#!/bin/sh
# sing-box manager Agent 接入脚本。
# 在运行 Agent（Claude Code、Codex 等）的电脑上执行，不要在网关服务器上执行（除非 Agent 就运行在服务器上）。
set -eu

SERVER=@SERVER@
VERSION=@VERSION@
BIN_DIR="${SBM_BIN_DIR:-$HOME/.local/bin}"

fail() { echo "错误：$*" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || fail "需要 curl"

case "$(uname -s)" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) fail "暂不支持 $(uname -s)，请在 macOS 或 Linux 上执行" ;;
esac
case "$(uname -m)" in
	x86_64|amd64) arch=amd64 ;;
	arm64|aarch64) arch=arm64 ;;
	*) fail "暂不支持 $(uname -m) 架构" ;;
esac

echo "[1/3] 从管理器 $SERVER 下载 sbm v$VERSION（$os/$arch）"
mkdir -p "$BIN_DIR"
tmp="$(mktemp "$BIN_DIR/.sbm.XXXXXX")"
trap 'rm -f "$tmp"' EXIT INT TERM
code="$(@CURL@ -o "$tmp" -w '%{http_code}' "$SERVER/agent/download/sbm-$os-$arch")" || fail "无法连接管理器 $SERVER"
[ "$code" = 200 ] || fail "下载 sbm 失败（HTTP $code）：$(head -c 600 "$tmp")"
chmod 755 "$tmp"
"$tmp" ctl help >/dev/null 2>&1 || fail "下载的 sbm 无法在本机运行"
mv -f "$tmp" "$BIN_DIR/sbm"
echo "      已安装到 $BIN_DIR/sbm"

case ":$PATH:" in
	*":$BIN_DIR:"*) ;;
	*)
		case "${SHELL:-}" in
			*/zsh) rc="$HOME/.zshrc" ;;
			*/bash) if [ "$os" = darwin ]; then rc="$HOME/.bash_profile"; else rc="$HOME/.bashrc"; fi ;;
			*) rc="$HOME/.profile" ;;
		esac
		if ! grep -qs '# sbm-agent-path' "$rc"; then
			printf '\nexport PATH="%s:$PATH" # sbm-agent-path\n' "$BIN_DIR" >> "$rc"
			echo "      已把 $BIN_DIR 加入 PATH（写入 $rc），新开的终端和 Agent 会话生效"
		fi
		;;
esac

echo "[2/3] 安装 Agent Skill"
"$BIN_DIR/sbm" ctl skill install >/dev/null

echo "[3/3] 连接管理器"
if [ -r /dev/tty ]; then
	"$BIN_DIR/sbm" ctl connect @CONNECT@ </dev/tty
else
	echo "当前没有可交互终端。请在终端执行：$BIN_DIR/sbm ctl connect @CONNECT@"
	exit 1
fi
echo
echo "完成。重新打开 Claude Code 或 Codex，对它说「用 sbm 看看网关状态」即可开始。"
`
