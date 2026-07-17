package model

import (
	"strings"
	"unicode"
)

// Region heuristically tags a node by its display name (task 2.8): regional
// indicator flag emoji win, then a keyword table. Returns a short code (US, UK,
// JP, ...) or "" when nothing matches.
//
// Short ASCII codes (US, UK, ...) are matched against whole tokens, never as
// substrings, so "Australia" does not spuriously match "US". CJK and multi-word
// phrases are matched as case-insensitive substrings, which is safe for them.
func Region(name string) string {
	if code := flagRegion(name); code != "" {
		return code
	}
	tokens := tokenSet(name)
	lower := strings.ToLower(name)
	for _, e := range keywordTable {
		for _, code := range e.codes {
			if tokens[code] {
				return e.code
			}
		}
		for _, phrase := range e.phrases {
			if strings.Contains(name, phrase) || strings.Contains(lower, strings.ToLower(phrase)) {
				return e.code
			}
		}
	}
	return ""
}

// tokenSet splits name on non-alphanumeric runes and returns the uppercased
// tokens as a set, for exact code matching.
func tokenSet(name string) map[string]bool {
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[strings.ToUpper(f)] = true
	}
	return set
}

var isoToCode = map[string]string{
	"US": "US", "GB": "UK", "JP": "JP", "KR": "KR", "HK": "HK",
	"TW": "TW", "SG": "SG", "DE": "DE", "FR": "FR", "CA": "CA", "AU": "AU",
}

// flagRegion finds the first regional-indicator pair in s and decodes it to an
// ISO country code, then to our display code.
func flagRegion(s string) string {
	runes := []rune(s)
	for i := 0; i+1 < len(runes); i++ {
		a, b := runes[i], runes[i+1]
		if isRegionalIndicator(a) && isRegionalIndicator(b) {
			iso := string(rune('A'+(a-0x1F1E6))) + string(rune('A'+(b-0x1F1E6)))
			if code, ok := isoToCode[iso]; ok {
				return code
			}
		}
	}
	return ""
}

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

type regionEntry struct {
	code    string
	codes   []string // exact-token ASCII codes
	phrases []string // substring phrases (CJK / multi-word)
}

// keywordTable covers the regions named in task 2.8.
var keywordTable = []regionEntry{
	{"US", []string{"US", "USA"}, []string{"美国", "美國", "United States", "洛杉矶", "圣何塞", "西雅图", "纽约", "达拉斯", "硅谷"}},
	{"UK", []string{"UK", "GB"}, []string{"英国", "英國", "United Kingdom", "伦敦", "London"}},
	{"JP", []string{"JP"}, []string{"日本", "Japan", "东京", "大阪", "Tokyo", "Osaka"}},
	{"KR", []string{"KR"}, []string{"韩国", "韓國", "Korea", "首尔", "Seoul"}},
	{"HK", []string{"HK"}, []string{"香港", "Hong Kong", "HongKong"}},
	{"TW", []string{"TW"}, []string{"台湾", "臺灣", "台灣", "Taiwan", "台北", "Taipei"}},
	{"SG", []string{"SG"}, []string{"新加坡", "狮城", "獅城", "Singapore"}},
	{"DE", []string{"DE"}, []string{"德国", "德國", "Germany", "法兰克福", "Frankfurt"}},
	{"FR", []string{"FR"}, []string{"法国", "法國", "France", "巴黎", "Paris"}},
	{"CA", []string{"CA"}, []string{"加拿大", "Canada", "多伦多", "Toronto"}},
	{"AU", []string{"AU"}, []string{"澳大利亚", "澳洲", "Australia", "悉尼", "Sydney"}},
}
