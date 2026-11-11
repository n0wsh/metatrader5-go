package mt5legacy

import "strings"

// matchGroup applies the Python package's symbol group filter: a
// comma-separated list of case-sensitive '*' wildcards, where a mask prefixed
// with '!' excludes. Any matching exclusion rejects the symbol regardless of
// order; otherwise any matching inclusion accepts it.
func matchGroup(group, symbol string) bool {
	matched := false
	for _, mask := range strings.Split(group, ",") {
		mask = strings.TrimSpace(mask)
		if mask == "" {
			continue
		}
		if strings.HasPrefix(mask, "!") {
			if wildcard(strings.TrimLeft(mask, "!"), symbol) {
				return false
			}
			continue
		}
		if wildcard(mask, symbol) {
			matched = true
		}
	}
	return matched
}

// wildcard reports whether s matches pattern, where '*' matches any run.
func wildcard(pattern, s string) bool {
	p, i := 0, 0
	star, mark := -1, 0
	for i < len(s) {
		switch {
		case p < len(pattern) && pattern[p] == '*':
			star, mark = p, i
			p++
		case p < len(pattern) && pattern[p] == s[i]:
			p++
			i++
		case star >= 0:
			p = star + 1
			mark++
			i = mark
		default:
			return false
		}
	}
	for p < len(pattern) && pattern[p] == '*' {
		p++
	}
	return p == len(pattern)
}
