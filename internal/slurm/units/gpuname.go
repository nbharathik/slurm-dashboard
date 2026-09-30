package units

import (
	"regexp"
	"strings"
)

// gpuRules map a normalised GPU type to a short display name; first match wins.
var gpuRules = []struct {
	re   *regexp.Regexp
	name func(m []string) string
}{
	{regexp.MustCompile(`\brtx pro (\d{4}) blackwell\b`), func(m []string) string { return "RTX PRO " + m[1] + " BW" }},
	{regexp.MustCompile(`\brtx (\d{4}) ada\b`), func(m []string) string { return "RTX " + m[1] + " Ada" }},
	{regexp.MustCompile(`\brtx a(\d{4})\b`), func(m []string) string { return "RTX A" + m[1] }},
	{regexp.MustCompile(`\b((?:gh|gb|b|h)\d{3})\b.*\bnvl\b`), func(m []string) string { return strings.ToUpper(m[1]) + " NVL" }},
	{regexp.MustCompile(`\b((?:gh|gb|b|h|a|v)\d{3})\b.*?\b(\d+) ?gb\b`), func(m []string) string { return strings.ToUpper(m[1]) + " " + m[2] + "GB" }},
	{regexp.MustCompile(`\b((?:gh|gb|b|h|a|v)\d{3})\b`), func(m []string) string { return strings.ToUpper(m[1]) }},
	{regexp.MustCompile(`\b(l40s|l40|l4|a40|a30|a16|a10|a2|t4|p100|p40|k80)\b`), func(m []string) string { return strings.ToUpper(m[1]) }},
	{regexp.MustCompile(`\bmi(\d{3})(x|a)?\b`), func(m []string) string { return strings.ToUpper("mi" + m[1] + m[2]) }},
}

var vendorWords = map[string]bool{"nvidia": true, "tesla": true, "amd": true, "instinct": true, "geforce": true}

// GPUDisplayName shortens a GPU type for tables ("nvidia_h200_nvl" gives "H200 NVL").
// Aliases win (case-insensitive); MIG slices keep their profile; unknown types are upper-cased if short, else title case.
func GPUDisplayName(raw string, aliases map[string]string) string {
	raw = strings.TrimSpace(raw)
	for k, v := range aliases {
		if strings.EqualFold(k, raw) && v != "" {
			return v
		}
	}
	if raw == "" {
		return ""
	}
	if loc := migProfile.FindStringIndex(raw); loc != nil {
		profile := strings.TrimLeft(raw[loc[0]:], "_-")
		if parent := strings.TrimRight(raw[:loc[0]], "_-"); parent != "" {
			return GPUDisplayName(parent, aliases) + " " + profile
		}
		return "MIG " + profile
	}
	var words []string
	for _, w := range strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool { return r == '_' || r == '-' || r == ' ' }) {
		if !vendorWords[w] {
			words = append(words, w)
		}
	}
	norm := strings.Join(words, " ")
	for _, r := range gpuRules {
		if m := r.re.FindStringSubmatch(norm); m != nil {
			return r.name(m)
		}
	}
	if len(raw) <= 10 {
		return strings.ToUpper(raw)
	}
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// GPUDisplayNames shortens a comma-separated list of types.
func GPUDisplayNames(raw string, aliases map[string]string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		parts[i] = GPUDisplayName(p, aliases)
	}
	return strings.Join(parts, ", ")
}
