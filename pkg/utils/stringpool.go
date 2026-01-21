package utils

import "sync"

// stringPool 字符串驻留池，用于减少重复字符串的内存占用
type stringPool struct {
	pool sync.Map
}

var (
	// countryPool 国家代码字符串池
	countryPool = &stringPool{}
	// emojiPool 国家表情符号字符串池
	emojiPool = &stringPool{}
)

// Intern 驻留字符串，返回池中的共享实例
func (p *stringPool) Intern(s string) string {
	if s == "" {
		return ""
	}

	// 尝试从池中获取
	if v, ok := p.pool.Load(s); ok {
		return v.(string)
	}

	// 存储到池中
	p.pool.Store(s, s)
	return s
}

// InternCountry 驻留国家代码字符串
// 用于优化 Node.Country 字段的内存占用
func InternCountry(code string) string {
	return countryPool.Intern(code)
}

// InternEmoji 驻留表情符号字符串
// 用于优化 Node.CountryEmoji 字段的内存占用
func InternEmoji(emoji string) string {
	return emojiPool.Intern(emoji)
}
