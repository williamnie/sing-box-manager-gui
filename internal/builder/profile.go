package builder

import "regexp"

var singBoxVersionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)

// CompatProfile 表示针对特定 sing-box 版本的配置生成能力。
type CompatProfile struct {
	LegacyInboundFields bool
}

// DefaultCompatProfile 默认按现代 sing-box 配置格式生成。
func DefaultCompatProfile() CompatProfile {
	return CompatProfile{}
}

// CompatProfileFromVersion 根据 sing-box version 输出推导兼容配置。
func CompatProfileFromVersion(versionOutput string) CompatProfile {
	matches := singBoxVersionPattern.FindStringSubmatch(versionOutput)
	if len(matches) != 4 {
		return DefaultCompatProfile()
	}

	major := parseVersionPart(matches[1])
	minor := parseVersionPart(matches[2])

	if major < 1 || (major == 1 && minor < 13) {
		return CompatProfile{
			LegacyInboundFields: true,
		}
	}

	return DefaultCompatProfile()
}

func parseVersionPart(value string) int {
	result := 0
	for _, ch := range value {
		result = result*10 + int(ch-'0')
	}
	return result
}
