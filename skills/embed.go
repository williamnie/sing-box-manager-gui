// Package skills 把 Agent Skill 编入 sbm，Agent 电脑执行 sbm ctl skill install 即可安装，无需克隆仓库。
package skills

import "embed"

//go:embed sing-box-manager
var FS embed.FS
