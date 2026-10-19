package ctl

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runSkill(t *testing.T, vars map[string]string, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(append([]string{"skill"}, args...), &out, &errOut, strings.NewReader(""), func(key string) string { return vars[key] })
	return code, out.String() + errOut.String()
}

func TestSkillInstallNeedsNoConnectionAndTargetsExistingAgents(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"HOME": home, "XDG_STATE_HOME": filepath.Join(home, "state")}
	code, output := runSkill(t, vars, "install")
	if code != ExitOK {
		t.Fatal(code, output)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "sing-box-manager", "SKILL.md")); err != nil {
		t.Fatal("Codex skill missing", output)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("must not create Claude Code directory when only Codex exists")
	}
	for _, rel := range []string{"references/ops.md", "references/concepts.md"} {
		if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "sing-box-manager", rel)); err != nil {
			t.Fatal("embedded reference missing", rel)
		}
	}
}

func TestSkillInstallDefaultsToClaudeAndBacksUpOutsideSkills(t *testing.T) {
	home := t.TempDir()
	vars := map[string]string{"HOME": home, "SBM_SKILL_BACKUP_DIR": filepath.Join(home, "backups")}
	if code, output := runSkill(t, vars, "install"); code != ExitOK {
		t.Fatal(code, output)
	}
	dest := filepath.Join(home, ".claude", "skills", "sing-box-manager")
	if err := os.WriteFile(filepath.Join(dest, "mine.md"), []byte("user note"), 0644); err != nil {
		t.Fatal(err)
	}
	code, output := runSkill(t, vars, "install")
	if code != ExitOK || !strings.Contains(output, "backups") {
		t.Fatal(code, output)
	}
	if _, err := os.Stat(filepath.Join(dest, "mine.md")); !os.IsNotExist(err) {
		t.Fatal("old files must be replaced")
	}
	backups, _ := filepath.Glob(filepath.Join(home, "backups", "sing-box-manager.*", "mine.md"))
	if len(backups) != 1 {
		t.Fatal("old version not backed up", backups)
	}
	entries, _ := os.ReadDir(filepath.Join(home, ".claude", "skills"))
	if len(entries) != 1 {
		t.Fatal("temporary or backup directory left in skills", entries)
	}
}

func TestSkillInstallExplicitDirAndUsage(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	if code, output := runSkill(t, map[string]string{"HOME": home}, "install", "--dir", dir); code != ExitOK {
		t.Fatal(code, output)
	}
	if _, err := os.Stat(filepath.Join(dir, "sing-box-manager", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("explicit dir must not install defaults")
	}
	if code, _ := runSkill(t, map[string]string{"HOME": home}, "remove"); code != ExitUsage {
		t.Fatal("unknown skill subcommand accepted")
	}
}
