// Package runtimecontrol 仅适配受管 sing-box 的运行控制接口，不提供通用 HTTP 反代。
package runtimecontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	ErrInvalidRequest   = errors.New("运行控制请求无效")
	ErrInvalidTestURL   = fmt.Errorf("%w：测速地址无效", ErrInvalidRequest)
	ErrUnsupported      = errors.New("内核不支持所需运行控制接口")
	ErrUnavailable      = errors.New("无法连接运行中的内核控制接口")
	ErrUnauthorized     = errors.New("内核控制接口认证失败")
	ErrNotFound         = errors.New("运行中的代理组或节点不存在")
	ErrSelectionChanged = errors.New("内核当前选择与请求不一致，请刷新后重试")
)

const (
	maxResponseBytes = 8 << 20
	requestTimeout   = 12 * time.Second
)

// 多个配置快照共享连接池；密钥只放在单次请求头中。
var controllerHTTP = &http.Client{
	Timeout: requestTimeout,
	Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: requestTimeout,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       16,
		DisableCompression:    true,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type Proxy struct {
	Tag        string   `json:"tag"`
	Type       string   `json:"type"`
	Selectable bool     `json:"selectable"`
	Members    []string `json:"members"`
	Selected   string   `json:"selected"`
	Delay      *int     `json:"delay"`
}

type Connection struct {
	ID              string   `json:"id"`
	Source          string   `json:"source"`
	SourcePort      string   `json:"source_port"`
	Destination     string   `json:"destination"`
	DestinationPort string   `json:"destination_port"`
	Host            string   `json:"host"`
	Network         string   `json:"network"`
	Protocol        string   `json:"protocol"`
	Process         string   `json:"process"`
	Chains          []string `json:"chains"`
	Rule            string   `json:"rule"`
	StartedAt       string   `json:"started_at"`
	Upload          int64    `json:"upload"`
	Download        int64    `json:"download"`
}

type Connections struct {
	UploadTotal   int64        `json:"upload_total"`
	DownloadTotal int64        `json:"download_total"`
	Connections   []Connection `json:"connections"`
}

type Client struct {
	base     *url.URL
	secret   string
	http     *http.Client
	lookupIP func(context.Context, string, string) ([]net.IP, error)
}

// NewClient 的 controller 必须来自受管进程实际使用的配置，格式为 IP:port。
// wildcard 监听地址映射到同地址族的 loopback，禁止域名、URL 和远程地址。
func NewClient(controller, secret string) (*Client, error) {
	host, port, err := net.SplitHostPort(controller)
	if err != nil || strings.ContainsAny(secret, "\r\n") {
		return nil, ErrInvalidRequest
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, ErrInvalidRequest
	}
	if host == "" {
		host = "127.0.0.1"
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return nil, ErrInvalidRequest
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() {
		if ip.Is4() {
			ip = netip.MustParseAddr("127.0.0.1")
		} else {
			ip = netip.IPv6Loopback()
		}
	}
	if !ip.IsLoopback() {
		return nil, ErrInvalidRequest
	}
	return &Client{
		base:     &url.URL{Scheme: "http", Host: net.JoinHostPort(ip.String(), strconv.Itoa(portNumber))},
		secret:   secret,
		http:     controllerHTTP,
		lookupIP: net.DefaultResolver.LookupIP,
	}, nil
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var response struct {
		Version string `json:"version"`
	}
	if err := c.request(ctx, http.MethodGet, "/version", nil, nil, &response); err != nil {
		return "", err
	}
	if response.Version == "" {
		return "", ErrUnsupported
	}
	return response.Version, nil
}

type upstreamProxy struct {
	Type    string   `json:"type"`
	All     []string `json:"all"`
	Now     string   `json:"now"`
	History []struct {
		Delay *int `json:"delay"`
	} `json:"history"`
}

func (c *Client) Proxies(ctx context.Context) ([]Proxy, error) {
	var response struct {
		Proxies map[string]upstreamProxy `json:"proxies"`
	}
	if err := c.request(ctx, http.MethodGet, "/proxies", nil, nil, &response); err != nil {
		return nil, err
	}
	if response.Proxies == nil {
		return nil, ErrUnsupported
	}
	proxies := make([]Proxy, 0, len(response.Proxies))
	for tag, upstream := range response.Proxies {
		members := upstream.All
		if members == nil {
			members = []string{}
		}
		proxy := Proxy{
			Tag: tag, Type: upstream.Type, Selectable: strings.EqualFold(upstream.Type, "selector"),
			Members: members, Selected: upstream.Now,
		}
		if len(upstream.History) > 0 {
			proxy.Delay = positiveDelay(upstream.History[len(upstream.History)-1].Delay)
		}
		proxies = append(proxies, proxy)
	}
	// map 的遍历顺序不稳定，固定输出避免页面每次刷新跳动。
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].Tag < proxies[j].Tag })
	return proxies, nil
}

func (c *Client) Select(ctx context.Context, group, member string) error {
	if !validTag(group) || !validTag(member) {
		return ErrInvalidRequest
	}
	proxies, err := c.Proxies(ctx)
	if err != nil {
		return err
	}
	var selected *Proxy
	for i := range proxies {
		if proxies[i].Tag == group {
			selected = &proxies[i]
			break
		}
	}
	if selected == nil {
		return ErrNotFound
	}
	if !selected.Selectable {
		return fmt.Errorf("%w：仅手动选择组支持切换", ErrInvalidRequest)
	}
	memberExists := false
	for _, candidate := range selected.Members {
		if candidate == member {
			memberExists = true
			break
		}
	}
	if !memberExists {
		return fmt.Errorf("%w：节点不属于当前代理组", ErrInvalidRequest)
	}
	if selected.Selected == member {
		return nil
	}
	body := struct {
		Name string `json:"name"`
	}{Name: member}
	if err := c.request(ctx, http.MethodPut, "/proxies/"+escapeTag(group), nil, body, nil); err != nil {
		return err
	}
	proxies, err = c.Proxies(ctx)
	if err != nil {
		return err
	}
	for _, proxy := range proxies {
		if proxy.Tag == group && proxy.Selected == member {
			return nil
		}
	}
	return ErrSelectionChanged
}

func (c *Client) Delay(ctx context.Context, tag, testURL string, timeoutMS int) (*int, error) {
	if !validTag(tag) || timeoutMS < 100 || timeoutMS > 10000 {
		return nil, ErrInvalidRequest
	}
	if err := c.validateTestURL(ctx, testURL); err != nil {
		return nil, err
	}
	query := url.Values{"url": {testURL}, "timeout": {strconv.Itoa(timeoutMS)}}
	var response struct {
		Delay json.RawMessage `json:"delay"`
	}
	if err := c.request(ctx, http.MethodGet, "/proxies/"+escapeTag(tag)+"/delay", query, nil, &response); err != nil {
		return nil, err
	}
	if len(response.Delay) == 0 {
		return nil, ErrUnsupported
	}
	var delay *int
	if err := json.Unmarshal(response.Delay, &delay); err != nil {
		return nil, ErrUnsupported
	}
	return positiveDelay(delay), nil
}

func positiveDelay(delay *int) *int {
	if delay == nil || *delay <= 0 {
		return nil
	}
	return delay
}

// stringNumber 兼容 sing-box 的字符串端口与其他版本的数值端口，缺失值不伪造为 0。
type stringNumber string

func (value *stringNumber) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*value = ""
		return nil
	}
	var text string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
	} else {
		var number json.Number
		if err := json.Unmarshal(data, &number); err != nil {
			return err
		}
		text = number.String()
	}
	*value = stringNumber(text)
	return nil
}

func (c *Client) Connections(ctx context.Context) (Connections, error) {
	var response struct {
		UploadTotal   *int64 `json:"uploadTotal"`
		DownloadTotal *int64 `json:"downloadTotal"`
		Connections   []struct {
			ID       string `json:"id"`
			Metadata struct {
				SourceIP        string       `json:"sourceIP"`
				SourcePort      stringNumber `json:"sourcePort"`
				DestinationIP   string       `json:"destinationIP"`
				DestinationPort stringNumber `json:"destinationPort"`
				Host            string       `json:"host"`
				Network         string       `json:"network"`
				Type            string       `json:"type"`
				Protocol        string       `json:"protocol"`
				ProcessPath     string       `json:"processPath"`
				Process         string       `json:"process"`
			} `json:"metadata"`
			Chains   []string `json:"chains"`
			Rule     string   `json:"rule"`
			Start    string   `json:"start"`
			Upload   int64    `json:"upload"`
			Download int64    `json:"download"`
		} `json:"connections"`
	}
	if err := c.request(ctx, http.MethodGet, "/connections", nil, nil, &response); err != nil {
		return Connections{}, err
	}
	if response.UploadTotal == nil || response.DownloadTotal == nil {
		return Connections{}, ErrUnsupported
	}
	result := Connections{
		UploadTotal: *response.UploadTotal, DownloadTotal: *response.DownloadTotal,
		Connections: make([]Connection, 0, len(response.Connections)),
	}
	for _, upstream := range response.Connections {
		metadata := upstream.Metadata
		protocol := metadata.Protocol
		if protocol == "" {
			protocol = metadata.Type
		}
		process := metadata.ProcessPath
		if process == "" {
			process = metadata.Process
		}
		chains := upstream.Chains
		if chains == nil {
			chains = []string{}
		}
		result.Connections = append(result.Connections, Connection{
			ID: upstream.ID, Source: metadata.SourceIP, SourcePort: string(metadata.SourcePort),
			Destination: metadata.DestinationIP, DestinationPort: string(metadata.DestinationPort),
			Host: metadata.Host, Network: metadata.Network, Protocol: protocol, Process: process,
			Chains: chains, Rule: upstream.Rule, StartedAt: upstream.Start, Upload: upstream.Upload, Download: upstream.Download,
		})
	}
	return result, nil
}

// Close 仅关闭一条已知连接；空 ID 不转换为关闭全部，以免触发内核网络重置。
func (c *Client) Close(ctx context.Context, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || id != parsed.String() {
		return ErrInvalidRequest
	}
	return c.request(ctx, http.MethodDelete, "/connections/"+id, nil, nil, nil)
}

func validTag(tag string) bool {
	if tag == "" || len(tag) > 1024 {
		return false
	}
	for _, char := range tag {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func escapeTag(tag string) string {
	if tag == "." || tag == ".." {
		return strings.ReplaceAll(tag, ".", "%2E")
	}
	return url.PathEscape(tag)
}

// 错误只带固定描述与状态码，不返回上游 body、URL、请求头或底层错误的敏感字段。
func (c *Client) request(ctx context.Context, method, escapedPath string, query url.Values, body, result any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return ErrInvalidRequest
		}
		reader = bytes.NewReader(encoded)
	}
	endpoint := c.base.String() + escapedPath
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return ErrInvalidRequest
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.secret != "" {
		request.Header.Set("Authorization", "Bearer "+c.secret)
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return errors.Join(ErrUnavailable, ctx.Err())
		}
		return ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ErrUnauthorized
		case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
			return ErrUnsupported
		case http.StatusBadRequest:
			return ErrInvalidRequest
		default:
			return fmt.Errorf("%w (HTTP %d)", ErrUnavailable, response.StatusCode)
		}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return errors.Join(ErrUnavailable, ctx.Err())
		}
		return ErrUnavailable
	}
	if len(data) > maxResponseBytes {
		return fmt.Errorf("%w：响应超出大小限制", ErrUnavailable)
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("%w：响应格式不兼容", ErrUnsupported)
		}
	}
	return nil
}

func (c *Client) validateTestURL(ctx context.Context, testURL string) error {
	parsed, err := url.Parse(testURL)
	if err != nil || len(testURL) > 2048 || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return ErrInvalidTestURL
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return ErrInvalidTestURL
		}
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicTestIP(ip) {
			return ErrInvalidTestURL
		}
		return nil
	}
	if localTestHostname(host) {
		return ErrInvalidTestURL
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := c.lookupIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w：无法确认测速地址，请检查 DNS 或更换可信 HTTPS 测速地址", ErrInvalidTestURL)
	}
	for _, ip := range ips {
		address, valid := netip.AddrFromSlice(ip)
		if !valid || (!publicTestIP(address) && !configuredFakeIP(address)) {
			return fmt.Errorf("%w：测速地址必须是公网 HTTPS 域名或地址", ErrInvalidTestURL)
		}
	}
	// 系统 DNS 可能经过本项目的 FakeIP 服务；它返回的占位地址不代表目标为内网。
	// 仅域名解析结果允许已知 FakeIP 池，字面 IP 仍必须通过 publicTestIP 的检查。
	// 原 HTTPS 域名交给所选 outbound 后由内核解析，内核也可能跟随重定向；
	// 这里是尽力预检查，不能固定 DNS，也不能作为 SSRF 隔离保证。仅使用可信测速服务。
	return nil
}

func localTestHostname(host string) bool {
	if !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{"localhost", "local", "localdomain", "lan", "home", "home.arpa", "internal", "intranet"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// 与 builder 生成的 dns_fakeip 地址池保持一致；不接受池外的 RFC1918 或 IPv6 ULA。
var configuredFakeIPNetworks = []netip.Prefix{
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("fc00::/18"),
}

func configuredFakeIP(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	for _, block := range configuredFakeIPNetworks {
		if block.Contains(address) {
			return true
		}
	}
	return false
}

var nonPublicTestNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func publicTestIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, block := range nonPublicTestNetworks {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}
