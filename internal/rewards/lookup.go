package rewards

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/happyro/happyro-api/internal/catalog"
	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type TargetReader interface {
	Targets(context.Context, string) ([]database.Row, error)
}
type ItemSearch interface {
	Items(context.Context, catalog.ItemQuery) (catalog.Page, error)
}
type Lookup struct {
	Targets TargetReader
	Items   ItemSearch
}

func searchTerm(value string, maximum int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum || strings.ContainsRune(value, 0) {
		return "", invalid("请输入有效的搜索内容。")
	}
	return value, nil
}
func (s *Lookup) FindTargets(ctx context.Context, query string) ([]database.Row, error) {
	query, err := searchTerm(query, 64)
	if err != nil {
		return nil, err
	}
	return s.Targets.Targets(ctx, query)
}
func (s *Lookup) FindItems(ctx context.Context, query string) ([]catalog.Object, error) {
	query, err := searchTerm(query, 100)
	if err != nil {
		return nil, err
	}
	page, err := s.Items.Items(ctx, catalog.ItemQuery{Search: query, Range: "server", DropKind: "all", Pagination: database.Pagination{Page: 1, Size: 20}})
	if err != nil {
		return nil, err
	}
	result := make([]catalog.Object, 0, len(page.Data))
	for _, item := range page.Data {
		result = append(result, catalog.Object{"item_id": item["Id"], "aegis_name": item["AegisName"], "names": item["names"], "type": item["Type"]})
	}
	return result, nil
}

type SQLTargets struct{ DB *sqlx.DB }

func (r *SQLTargets) Targets(ctx context.Context, query string) ([]database.Row, error) {
	escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(query)
	id, _ := strconv.ParseInt(query, 10, 32)
	return database.Rows(ctx, r.DB, "SELECT c.char_id,c.name,l.userid AS username FROM `char` c JOIN login l ON l.account_id=c.account_id WHERE l.sex<>'S' AND (c.char_id=? OR c.name LIKE ? ESCAPE '!' OR l.userid LIKE ? ESCAPE '!') ORDER BY CASE WHEN c.char_id=? THEN 0 WHEN c.name=? THEN 1 WHEN l.userid=? THEN 2 ELSE 3 END,c.char_id DESC LIMIT 20", id, "%"+escaped+"%", "%"+escaped+"%", id, query, query)
}
