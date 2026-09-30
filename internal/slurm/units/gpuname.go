package units

import (
	"regexp"
	"strings"
)

// gpuRules turn a normalised GPU type (lower case, words separated by
// spaces, vendor names removed) into a short display name. The first
// matching rule wins.
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

// GPUDisplayName shortens a Slurm GPU type for tables: "nvidia_h200_nvl"
// becomes "H200 NVL" and
// "nvidia_rtx_pro_6000_blackwell_max-q_workstation_edition" becomes
// "RTX PRO 6000 BW". aliases (the [gpu] aliases config) win, matched
// case-insensitively. MIG slices keep their profile: "A100 3g.39gb", or
// "MIG 1g.33gb" when the parent GPU is not named. Unknown types are shown
// upper-cased when short, else in title case.
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
