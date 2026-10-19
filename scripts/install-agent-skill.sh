#!/bin/sh
# 从源码仓库安装 sing-box-manager Agent Skill。Agent 电脑上一般无需克隆仓库：
# 执行管理页面「Agent 接入」的一键命令，或直接运行 sbm ctl skill install（Skill 已编入 sbm）。
# 本脚本默认装到 Claude Code 用户级目录；
# 传入目录参数可安装到项目级 .claude/skills 或 Codex 的 skills 目录。
# 已有版本先完整备份到 skills 目录之外（避免被当成重复的 Skill 加载），再原子替换。
set -eu

src="$(cd "$(dirname "$0")/.." && pwd)/skills/sing-box-manager"
target="${1:-$HOME/.claude/skills}"
backup_root="${SBM_SKILL_BACKUP_DIR:-${XDG_STATE_HOME:-$HOME/.local/state}/sbm-skill-backups}"

if [ ! -f "$src/SKILL.md" ]; then
	echo "找不到 $src/SKILL.md" >&2
	exit 1
fi
mkdir -p "$target"
dest="$target/sing-box-manager"
tmp="$(mktemp -d "$target/.sing-box-manager.XXXXXX")"
backup=""
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT INT TERM
cp -R "$src/." "$tmp/"

if [ -e "$dest" ]; then
	mkdir -p "$backup_root"
	backup="$(mktemp -d "$backup_root/sing-box-manager.$(date +%Y%m%d%H%M%S).XXXXXX")"
	cp -R "$dest/." "$backup/"
	echo "已备份原有版本到 $backup"
	rm -rf "$dest"
fi
if ! mv "$tmp" "$dest"; then
	echo "安装失败" >&2
	if [ -n "$backup" ]; then
		cp -R "$backup" "$dest" && echo "已从备份恢复原有版本" >&2
	fi
	exit 1
fi
echo "已安装到 $dest"
if [ -n "$backup" ]; then
	echo "如需恢复旧版本：rm -rf \"$dest\" && cp -R \"$backup\" \"$dest\""
fi
if ! command -v sbm >/dev/null 2>&1; then
	echo "提示：PATH 中没有 sbm；Agent 所在机器需要能执行 sbm ctl（同一个 sbm 二进制）。" >&2
fi
