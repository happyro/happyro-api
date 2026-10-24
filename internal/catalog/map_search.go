package catalog

import "strings"

// A search result must contain the query in its currently displayed name.
func (c *Maps) matchesSearch(row Map, query string) bool {
	return contains(nameOf(row), strings.TrimSpace(query))
}

// Prefer exact displayed names, then prefixes, then substring matches.
func (c *Maps) searchRank(row Map, query string) int {
	term := strings.ToLower(strings.TrimSpace(query))
	name := strings.ToLower(nameOf(row))
	if term == "" || name == term {
		return 0
	}
	if strings.HasPrefix(name, term) {
		return 1
	}
	return 2
}
