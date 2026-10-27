package catalog

import "strings"

// Search current display names and stable map codes, never historical aliases.
func (c *Maps) matchesSearch(row Map, query string) bool {
	return contains(nameOf(row), strings.TrimSpace(query)) || contains(row.Map, strings.TrimSpace(query))
}

// Prefer exact names or codes, then prefixes, then substring matches.
func (c *Maps) searchRank(row Map, query string) int {
	term := strings.ToLower(strings.TrimSpace(query))
	name := strings.ToLower(nameOf(row))
	code := strings.ToLower(row.Map)
	if term == "" || name == term || code == term {
		return 0
	}
	if strings.HasPrefix(name, term) || strings.HasPrefix(code, term) {
		return 1
	}
	return 2
}
