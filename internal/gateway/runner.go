package gateway

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner 只供内部注入和隔离测试，HTTP 请求无法指定命令。
type Runner interface {
	Run(context.Context, string, []string, string) (string, error)
}
type SystemRunner struct{ paths map[string]string }

func NewSystemRunner() (*SystemRunner, error) {
	r := &SystemRunner{paths: map[string]string{}}
	for _, name := range []string{"ip", "ss", "nft", "sysctl", "systemctl"} {
		p := trustedExecutable(name)
		if p == "" {
			return nil, fmt.Errorf("缺少系统工具 %s", name)
		}
		r.paths[name] = p
	}
	for _, name := range []string{"dnsmasq", "iptables-save", "ip6tables-save"} {
		r.paths[name] = trustedExecutable(name)
	}
	return r, nil
}
func trustedExecutable(name string) string {
	for _, d := range []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"} {
		p, e := filepath.EvalSymlinks(filepath.Join(d, name))
		if e != nil {
			continue
		}
		if f, e := os.Stat(p); e == nil && f.Mode().IsRegular() && f.Mode().Perm()&0111 != 0 && trustedPath(p, false) == nil {
			return p
		}
	}
	return ""
}
func (r *SystemRunner) Run(ctx context.Context, name string, args []string, input string) (string, error) {
	p := r.paths[name]
	if p == "" {
		return "", fmt.Errorf("系统工具不可用: %s", name)
	}
	cmd := exec.CommandContext(ctx, p, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Stdin = strings.NewReader(input)
	var out limitedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s 执行失败: %w", name, err)
	}
	if out.overflow {
		return "", fmt.Errorf("%s 检查输出超出安全大小限制", name)
	}
	return out.String(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 2<<20 {
		b.overflow = true
	}
	if b.Len() < 2<<20 {
		left := (2 << 20) - b.Len()
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.Buffer.Write(p)
	}
	return n, nil
}
