package characters

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/happyro/happyro-api/internal/platform/database"
)

var ErrNotFound = errors.New("角色不存在。")
var ErrFilter = errors.New("查询参数无效。")

type Query struct {
	Search     string
	OnlineOnly bool
	Username   string
	Name       string
	Pagination database.Pagination
}

type Reader interface {
	Characters(context.Context, Query) (database.Page, error)
	Character(context.Context, int64) (database.Row, error)
}
type MapNames interface{ MapName(string) *string }
type Service struct {
	Reader Reader
	Maps   MapNames
}

func validFilter(value string) bool { return len(value) <= 256 && utf8.ValidString(value) }
func (s *Service) Characters(ctx context.Context, q Query) (database.Page, error) {
	if !q.Pagination.Valid() || !validFilter(q.Username) || !validFilter(q.Name) || !validFilter(q.Search) {
		return database.Page{}, ErrFilter
	}
	page, err := s.Reader.Characters(ctx, q)
	if err != nil {
		return database.Page{}, err
	}
	for _, character := range page.Data {
		s.addMapName(character)
	}
	return page, nil
}
func (s *Service) Character(ctx context.Context, id int64) (database.Row, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	character, err := s.Reader.Character(ctx, id)
	if err != nil {
		return nil, err
	}
	s.addMapName(character)
	return character, nil
}

func (s *Service) addMapName(character database.Row) {
	character["last_map_name"] = nil
	if s.Maps != nil {
		if name, ok := character["last_map"].(string); ok {
			character["last_map_name"] = s.Maps.MapName(name)
		}
	}
}
