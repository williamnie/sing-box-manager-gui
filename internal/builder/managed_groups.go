package builder

import (
	"fmt"
	"slices"
	"sort"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 国家组与 GLOBAL 由当前启用节点重建，不恢复任何旧导入分组。
func (b *ConfigBuilder) appendManagedGroups(outbounds []Outbound, nodes []string, countries map[string][]string, filters []string) []Outbound {
	codes := make([]string, 0, len(countries))
	for code := range countries {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	members := []string{"DIRECT", "REJECT", "Proxy"}
	for _, code := range codes {
		tags := countries[code]
		if len(tags) == 0 {
			continue
		}
		tag := fmt.Sprintf("%s %s", storage.GetCountryEmoji(code), storage.GetCountryName(code))
		preferred := b.settings.ProxyPlan.DefaultNode
		if !slices.Contains(tags, preferred) {
			preferred = tags[0]
		}
		outbounds = append(outbounds, Outbound{"tag": tag, "type": "selector", "outbounds": tags, "default": preferred})
		members = append(members, tag)
	}
	members = append(members, filters...)
	members = append(members, nodes...)
	// 自身不能成为成员，否则会产生循环；重名出站由统一校验拒绝。
	outbounds = append(outbounds, Outbound{"tag": "GLOBAL", "type": "selector", "outbounds": members, "default": "Proxy"})
	return outbounds
}
