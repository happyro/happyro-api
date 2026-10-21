package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("资料不存在。")
var ErrNotReady = errors.New("游戏资料尚未导入。")
var ErrQuery = errors.New("资料查询参数无效。")

type ItemQuery struct {
	Search, Type, Subtype, Range, DropKind string
	Pagination                             database.Pagination
}
type MonsterQuery struct {
	Search, Race, Element, Size, Kind string
	Pagination                        database.Pagination
}
type NPCQuery struct {
	Search, Map, Name, DisplayName, OnMap, CurrentMap string
	GameOnly                                          bool
	Pagination                                        database.Pagination
}
type Page struct {
	Data     []Object  `json:"data"`
	Total    int64     `json:"total"`
	Subtypes *[]string `json:"subtypes,omitempty"`
}
type Versions struct {
	Client    string              `json:"clientVersion"`
	Server    string              `json:"serverVersion"`
	Available map[string][]string `json:"available"`
}

type Repository interface {
	Items(context.Context, ItemQuery) (Page, error)
	Item(context.Context, int64, string) (Object, error)
	Monsters(context.Context, MonsterQuery) (Page, error)
	Monster(context.Context, int64) (Object, error)
	NPCs(context.Context, NPCQuery) (Page, error)
	NPCsOnMap(context.Context, string, bool) ([]Object, error)
	Versions(context.Context) (Versions, error)
}
type SQLRepository struct {
	DB      *sqlx.DB
	Aliases map[string]string
}

func (r *SQLRepository) generation(ctx context.Context) (int64, error) {
	var id int64
	err := r.DB.GetContext(ctx, &id, "SELECT g.id FROM catalog_generations g JOIN catalog_state s ON s.generation_id=g.id WHERE s.id=1")
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotReady
	}
	return id, err
}
func escape(value string) string {
	return strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value)
}
func like(value string) string { return "%" + escape(value) + "%" }
func payloads(ctx context.Context, db sqlx.QueryerContext, query string, args ...any) ([]Object, error) {
	rows, err := db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Object{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var payload Object
		if err := decode(raw, &payload); err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, rows.Err()
}
func (r *SQLRepository) page(ctx context.Context, table, where, order string, args, ordering []any, p database.Pagination) (Page, error) {
	page := Page{Data: []Object{}}
	if err := r.DB.GetContext(ctx, &page.Total, "SELECT COUNT(*) FROM "+table+where, args...); err != nil {
		return page, err
	}
	bound := append(append(append([]any{}, args...), ordering...), p.Size, p.Offset())
	rows, err := payloads(ctx, r.DB, "SELECT payload FROM "+table+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", bound...)
	page.Data = rows
	return page, err
}
func itemRange(value string) string {
	switch value {
	case "client":
		return " AND client_exists=1"
	case "server":
		return " AND server_exists=1"
	}
	return ""
}
func searchClause(value, idColumn string) (string, []any) {
	sql := " AND (name_zh_cn LIKE ? ESCAPE '!' OR name_en_us LIKE ? ESCAPE '!' OR aegis_name LIKE ? ESCAPE '!'"
	args := []any{like(value), like(value), like(value)}
	if id, err := strconv.ParseInt(value, 10, 32); err == nil && id > 0 {
		sql += " OR " + idColumn + "=?"
		args = append(args, id)
	}
	return sql + ")", args
}
func (r *SQLRepository) Items(ctx context.Context, q ItemQuery) (Page, error) {
	generation, err := r.generation(ctx)
	if err != nil {
		return Page{}, err
	}
	where := " WHERE generation_id=?" + itemRange(q.Range)
	args := []any{generation}
	if q.Type != "" {
		where += " AND item_type=?"
		args = append(args, q.Type)
	}
	subtypes := []string{}
	if q.Type != "" {
		if err = r.DB.SelectContext(ctx, &subtypes, "SELECT DISTINCT item_subtype FROM catalog_items"+where+" AND item_subtype IS NOT NULL AND item_subtype<>'' ORDER BY item_subtype", args...); err != nil {
			return Page{}, err
		}
	}
	if q.DropKind != "" && q.DropKind != "all" {
		where += " AND aegis_name IN (SELECT d.item_aegis_name FROM catalog_drops d JOIN catalog_monsters m ON m.generation_id=d.generation_id AND m.monster_id=d.monster_id WHERE d.generation_id=? AND m.kind=?)"
		args = append(args, generation, q.DropKind)
	}
	if q.Search != "" {
		clause, bound := searchClause(q.Search, "item_id")
		where += clause
		args = append(args, bound...)
	}
	if q.Subtype != "" {
		where += " AND item_subtype=?"
		args = append(args, q.Subtype)
	}
	page, err := r.page(ctx, "catalog_items", where, "item_id", args, nil, q.Pagination)
	page.Subtypes = &subtypes
	return page, err
}
func (r *SQLRepository) Item(ctx context.Context, id int64, scope string) (Object, error) {
	generation, err := r.generation(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := payloads(ctx, r.DB, "SELECT payload FROM catalog_items WHERE generation_id=? AND item_id=?"+itemRange(scope), generation, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	drops, err := r.dropSources(ctx, generation, text(rows[0]["AegisName"]))
	if err != nil {
		return nil, err
	}
	rows[0]["dropSources"] = drops
	return rows[0], nil
}
func (r *SQLRepository) Monsters(ctx context.Context, q MonsterQuery) (Page, error) {
	generation, err := r.generation(ctx)
	if err != nil {
		return Page{}, err
	}
	where := " WHERE generation_id=?"
	args := []any{generation}
	for _, filter := range []struct{ column, value string }{{"race", q.Race}, {"element", q.Element}, {"size", q.Size}, {"kind", q.Kind}} {
		if filter.value != "" && filter.value != "all" {
			where += " AND " + filter.column + "=?"
			args = append(args, filter.value)
		}
	}
	if q.Search != "" {
		clause, bound := searchClause(q.Search, "monster_id")
		where += clause
		args = append(args, bound...)
	}
	return r.page(ctx, "catalog_monsters", where, "monster_id", args, nil, q.Pagination)
}
func (r *SQLRepository) Monster(ctx context.Context, id int64) (Object, error) {
	generation, err := r.generation(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := payloads(ctx, r.DB, "SELECT payload FROM catalog_monsters WHERE generation_id=? AND monster_id=?", generation, id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	if err = r.localizeDrops(ctx, generation, rows[0]); err != nil {
		return nil, err
	}
	return rows[0], nil
}
func (r *SQLRepository) Versions(ctx context.Context) (Versions, error) {
	result := Versions{Available: map[string][]string{"client": {}, "server": {}}}
	err := r.DB.QueryRowxContext(ctx, "SELECT g.client_version,g.server_version FROM catalog_generations g JOIN catalog_state s ON s.generation_id=g.id WHERE s.id=1").Scan(&result.Client, &result.Server)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotReady
	}
	if err != nil {
		return result, err
	}
	for key, column := range map[string]string{"client": "client_version", "server": "server_version"} {
		values := []string{}
		if err = r.DB.SelectContext(ctx, &values, "SELECT "+column+" FROM catalog_generations GROUP BY "+column+" ORDER BY MAX(imported_at) DESC"); err != nil {
			return result, err
		}
		result.Available[key] = values
	}
	return result, nil
}
