package ctl

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobei/singbox-manager/skills"
)

const skillName = "sing-box-manager"

type stringList []string

func (l *stringList) String() string     { return strings.Join(*l, ",") }
func (l *stringList) Set(v string) error { *l = append(*l, v); return nil }

// skillTargets 默认安装到已存在的 Claude Code 和 Codex 目录；两者都没有时装到 Claude Code。
func skillTargets(e env) []string {
	home := firstNonEmpty(e.getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	claude := filepath.Join(home, ".claude")
	codex := firstNonEmpty(e.getenv("CODEX_HOME"), filepath.Join(home, ".codex"))
	var targets []string
	for _, root := range []string{claude, codex} {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			targets = append(targets, filepath.Join(root, "skills"))
		}
	}
	if len(targets) == 0 {
		targets = append(targets, filepath.Join(claude, "skills"))
	}
	return targets
}

func skillCommand(args []string, e env) int {
	if len(args) == 0 || args[0] != "install" {
		return usageErr(e, "用法: sbm ctl skill install [--dir 目录]...（默认安装到已存在的 ~/.claude/skills 和 ~/.codex/skills）")
	}
	fl := flag.NewFlagSet("skill install", flag.ContinueOnError)
	fl.SetOutput(e.stderr)
	var dirs stringList
	fl.Var(&dirs, "dir", "")
	if err := fl.Parse(args[1:]); err != nil || fl.NArg() != 0 {
		return usageErr(e, "用法: sbm ctl skill install [--dir 目录]...")
	}
	if len(dirs) == 0 {
		dirs = skillTargets(e)
	}
	home := firstNonEmpty(e.getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	backupRoot := firstNonEmpty(e.getenv("SBM_SKILL_BACKUP_DIR"), filepath.Join(firstNonEmpty(e.getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state")), "sbm-skill-backups"))
	var installed, backups []string
	for _, dir := range dirs {
		dest, backup, err := installSkill(dir, backupRoot)
		if err != nil {
			return fail(e, ExitUsage, fmt.Errorf("安装 Skill 到 %s 失败：%w", dir, err))
		}
		installed = append(installed, dest)
		if backup != "" {
			backups = append(backups, backup)
		}
		fmt.Fprintf(e.stderr, "Skill 已安装到 %s\n", dest)
	}
	data := map[string]any{"installed": installed}
	if len(backups) > 0 {
		data["backups"] = backups
	}
	out, _ := json.MarshalIndent(map[string]any{"data": data, "message": "Skill 已安装，重新打开 Agent 会话后生效"}, "", "  ")
	fmt.Fprintln(e.stdout, string(out))
	return ExitOK
}

// installSkill 先在目标目录旁写好新版本再替换；旧版本移到 skills 目录之外，避免被当成重复的 Skill 加载。
func installSkill(dir, backupRoot string) (string, string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	dest := filepath.Join(dir, skillName)
	tmp, err := os.MkdirTemp(dir, "."+skillName+".")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmp)
	if err := os.Chmod(tmp, 0755); err != nil {
		return "", "", err
	}
	err = fs.WalkDir(skills.FS, skillName, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(tmp, strings.TrimPrefix(path, skillName))
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		raw, err := skills.FS.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0644)
	})
	if err != nil {
		return "", "", err
	}
	backup := ""
	if _, err := os.Lstat(dest); err == nil {
		if err := os.MkdirAll(backupRoot, 0700); err != nil {
			return "", "", err
		}
		backup = filepath.Join(backupRoot, skillName+"."+time.Now().Format("20060102150405.000000000"))
		if err := moveDir(dest, backup); err != nil {
			return "", "", fmt.Errorf("备份旧版本失败：%w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if backup != "" {
			_ = moveDir(backup, dest)
		}
		return "", "", err
	}
	return dest, backup, nil
}

// moveDir 优先原子改名；备份目录可能在另一个文件系统，失败时复制后删除原目录。
func moveDir(from, to string) error {
	if os.Rename(from, to) == nil {
		return nil
	}
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	return os.RemoveAll(from)
}
