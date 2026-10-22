package catalog

import (
	"context"
	"strings"
)

var npcSearchColumns = []string{"display_name", "source_name", "name", "map_name", "map", "navigation_name", "navigation_class"}

func normalizeMap(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".gat")
}
func (r *SQLRepository) npcMatches(term, pattern, operator string, rank int) (string, []any) {
	clauses := []string{}
	args := []any{}
	for _, column := range npcSearchColumns {
		expression := "LOWER(" + column + ") " + operator + " ?"
		if operator == "LIKE" {
			expression += " ESCAPE '!'"
		}
		clauses = append(clauses, expression)
		args = append(args, pattern)
	}
	for alias, source := range r.Aliases {
		alias = strings.ToLower(alias)
		if !strings.Contains(alias, term) {
			continue
		}
		aliasRank := 2
		if alias == term {
			aliasRank = 0
		} else if strings.HasPrefix(alias, term) {
			aliasRank = 1
		}
		if rank >= 0 && aliasRank != rank {
			continue
		}
		clauses = append(clauses, "LOWER(navigation_name) LIKE ? ESCAPE '!'")
		args = append(args, like(strings.ToLower(source)))
	}
	return "(" + strings.Join(clauses, " OR ") + ")", args
}
func (r *SQLRepository) NPCs(ctx context.Context, q NPCQuery) (Page, error) {
	generation, err := r.generation(ctx)
	if err != nil {
		return Page{}, err
	}
	where := " WHERE generation_id=?"
	args := []any{generation}
	if q.GameOnly {
		where += " AND game_visible=1"
	}
	if q.OnMap != "" {
		where += " AND map=?"
		args = append(args, normalizeMap(q.OnMap))
	}
	if q.Map != "" {
		where += " AND (map LIKE ? ESCAPE '!' OR map_name LIKE ? ESCAPE '!')"
		args = append(args, like(q.Map), like(q.Map))
	}
	for _, filter := range []struct{ column, value string }{{"name", q.Name}, {"display_name", q.DisplayName}} {
		if filter.value != "" {
			where += " AND " + filter.column + " LIKE ? ESCAPE '!'"
			args = append(args, like(filter.value))
		}
	}
	if q.Search != "" {
		term := strings.ToLower(q.Search)
		clause, bound := r.npcMatches(term, like(term), "LIKE", -1)
		where += " AND " + clause
		args = append(args, bound...)
	}
	term := ""
	for _, candidate := range []string{q.DisplayName, q.Name, q.Map, q.Search} {
		if candidate != "" {
			term = strings.ToLower(strings.TrimSpace(candidate))
			break
		}
	}
	order := []string{}
	ordering := []any{}
	if q.CurrentMap != "" && (term != "" || q.OnMap != "") {
		order = append(order, "CASE WHEN map=? THEN 0 ELSE 1 END")
		ordering = append(ordering, normalizeMap(q.CurrentMap))
	}
	if term != "" {
		expression := "CASE "
		for rank, pattern := range []string{term, escape(term) + "%", like(term)} {
			operator := "LIKE"
			if rank == 0 {
				operator = "="
			}
			clause, bound := r.npcMatches(term, pattern, operator, rank)
			expression += "WHEN " + clause + " THEN " + []string{"0", "1", "2"}[rank] + " "
			ordering = append(ordering, bound...)
		}
		order = append(order, expression+"ELSE 3 END", "display_name")
	}
	order = append(order, "catalog_order", "npc_key")
	return r.page(ctx, "catalog_npcs", where, strings.Join(order, ","), args, ordering, q.Pagination)
}
func (r *SQLRepository) NPCsOnMap(ctx context.Context, name string, gameOnly bool) ([]Object, error) {
	generation, err := r.generation(ctx)
	if err != nil {
		return nil, err
	}
	query := "SELECT payload FROM catalog_npcs WHERE generation_id=? AND map=?"
	if gameOnly {
		query += " AND game_visible=1"
	}
	return payloads(ctx, r.DB, query+" ORDER BY display_name,x,y,npc_key", generation, normalizeMap(name))
}
