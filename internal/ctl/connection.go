package ctl

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

type savedConnection struct {
	Version     int    `json:"version"`
	Server      string `json:"server"`
	Fingerprint string `json:"fingerprint,omitempty"`
	CAFile      string `json:"ca_file,omitempty"`
	AllowHTTP   bool   `json:"allow_insecure_http,omitempty"`
	Token       string `json:"token"`
}

func connectionPath(e env) string {
	root := e.getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, _ := os.UserHomeDir()
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "sbm", "connection.json")
}

func readConnection(path string) (*savedConnection, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取已保存连接")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("连接文件必须是仅当前用户可读的普通文件，请检查 %s 的权限（chmod 600）", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取已保存连接")
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 16<<10))
	decoder.DisallowUnknownFields()
	var saved savedConnection
	if decoder.Decode(&saved) != nil || saved.Version != 1 || saved.Server == "" || !validToken(saved.Token) {
		return nil, fmt.Errorf("连接文件无效，请重新执行 sbm ctl connect")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("连接文件无效，请重新执行 sbm ctl connect")
	}
	return &saved, nil
}

// 显式完整连接参数保持旧行为；保存的凭据只用于原来的服务地址。
func applySavedConnection(o *options, explicit map[string]bool, path string) error {
	if explicit["server"] && explicit["token-file"] {
		return nil
	}
	saved, err := readConnection(path)
	if err != nil || saved == nil {
		return err
	}
	if explicit["server"] && strings.TrimRight(o.server, "/") != saved.Server {
		if !explicit["token-file"] {
			return fmt.Errorf("目标地址与已保存连接不同；请重新 connect 或显式指定 --token-file，未发送已保存令牌")
		}
		return nil
	}
	if !explicit["server"] {
		o.server = saved.Server
	}
	if !explicit["fingerprint"] {
		o.fingerprint = saved.Fingerprint
	}
	if !explicit["ca-file"] {
		o.caFile = saved.CAFile
	}
	if !explicit["allow-insecure-http"] {
		o.allowHTTP = saved.AllowHTTP
	}
	if !explicit["token-file"] {
		o.token = saved.Token
	}
	return nil
}

func validToken(token string) bool {
	return strings.HasPrefix(token, "sbm_") && len(token) <= 128 && !strings.ContainsAny(token, " \t\r\n")
}

func readConnectionToken(e env) (string, error) {
	fmt.Fprintln(e.stderr, "粘贴管理页面创建的令牌并按回车（终端输入不显示）：")
	var raw []byte
	var err error
	if file, ok := e.stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		raw, err = term.ReadPassword(int(file.Fd()))
		fmt.Fprintln(e.stderr)
	} else {
		var line string
		line, err = bufio.NewReader(io.LimitReader(e.stdin, 256)).ReadString('\n')
		raw = []byte(line)
		if errors.Is(err, io.EOF) {
			err = nil
		}
	}
	if err != nil {
		return "", fmt.Errorf("读取令牌失败")
	}
	token := strings.TrimSpace(string(raw))
	if !validToken(token) {
		return "", fmt.Errorf("令牌格式无效，请重新复制；不要输入管理密码")
	}
	return token, nil
}

func saveConnection(path string, saved savedConnection) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".connection-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(saved); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func connect(o options, explicit map[string]bool, e env) int {
	var err error
	if explicit["token-file"] {
		o.token, err = readTokenFile(o.tokenFile)
	} else {
		o.token, err = readConnectionToken(e)
	}
	if err != nil {
		return fail(e, ExitUsage, err)
	}
	c, err := newClient(o)
	if err != nil {
		return fail(e, ExitUsage, err)
	}
	response, err := c.do("GET", "/capabilities", nil, nil, "")
	if err != nil || response.status != 200 {
		return emit(e, response, err)
	}
	var capabilities struct {
		Data struct {
			Version string `json:"api_version"`
		} `json:"data"`
	}
	if json.Unmarshal(response.body, &capabilities) != nil || capabilities.Data.Version != "v1" {
		return fail(e, ExitServer, fmt.Errorf("目标服务未返回兼容的 Agent API，原连接未修改"))
	}
	if o.caFile != "" {
		o.caFile, err = filepath.Abs(o.caFile)
		if err != nil {
			return fail(e, ExitUsage, fmt.Errorf("证书路径无效"))
		}
	}
	path := connectionPath(e)
	saved := savedConnection{Version: 1, Server: strings.TrimRight(o.server, "/"), Fingerprint: o.fingerprint, CAFile: o.caFile, AllowHTTP: o.allowHTTP, Token: o.token}
	if err := saveConnection(path, saved); err != nil {
		return fail(e, ExitUsage, fmt.Errorf("保存连接失败：%w", err))
	}
	encoded, _ := json.MarshalIndent(map[string]any{"data": map[string]any{"connected": true, "server": saved.Server, "config_file": path}, "message": "连接已验证并保存。以后直接运行 sbm ctl status"}, "", "  ")
	fmt.Fprintln(e.stdout, string(encoded))
	return ExitOK
}
