package catalog

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"unicode/utf8"
)

type AssetStore interface{ Exists(string, int64) bool }
type FilesystemAssets struct{ ItemRoot, MonsterRoot, WorldRoot string }

func (a FilesystemAssets) Location(kind string, id int64) (string, string) {
	name := strconv.FormatInt(id, 10) + ".png"
	switch kind {
	case "icon":
		return a.ItemRoot, "icons/" + name
	case "illustration":
		return a.ItemRoot, "illustrations/" + name
	case "monster":
		return a.MonsterRoot, name
	case "npc":
		return a.WorldRoot, "npcs/" + name
	}
	return "", ""
}
func (a FilesystemAssets) Exists(kind string, id int64) bool {
	if id <= 0 {
		return false
	}
	directory, path := a.Location(kind, id)
	if directory == "" || path == "" {
		return false
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return false
	}
	defer root.Close()
	stat, err := root.Stat(path)
	return err == nil && stat.Mode().IsRegular()
}

type Service struct {
	Repository Repository
	Assets     AssetStore
}

func short(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}
func validRange(scope string) bool {
	return slices.Contains([]string{"client", "server", "all"}, scope)
}
func validKind(kind string) bool {
	return slices.Contains([]string{"", "all", "normal", "mini", "mvp"}, kind)
}
func (s *Service) Items(ctx context.Context, q ItemQuery) (Page, error) {
	if !q.Pagination.Valid() || !short(q.Search, 100) || !short(q.Type, 32) || !short(q.Subtype, 32) || !validRange(q.Range) || !validKind(q.DropKind) {
		return Page{}, ErrQuery
	}
	page, err := s.Repository.Items(ctx, q)
	if err != nil {
		return page, err
	}
	for _, row := range page.Data {
		s.itemAssets(row)
	}
	return page, nil
}
func (s *Service) Item(ctx context.Context, id int64, scope string) (Object, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	if !validRange(scope) {
		return nil, ErrQuery
	}
	row, err := s.Repository.Item(ctx, id, scope)
	if err != nil {
		return nil, err
	}
	s.itemAssets(row)
	return row, nil
}
func (s *Service) Monsters(ctx context.Context, q MonsterQuery) (Page, error) {
	if !q.Pagination.Valid() || !short(q.Search, 100) || !short(q.Race, 32) || !short(q.Element, 32) || !slices.Contains([]string{"", "Small", "Medium", "Large"}, q.Size) || !validKind(q.Kind) {
		return Page{}, ErrQuery
	}
	page, err := s.Repository.Monsters(ctx, q)
	if err != nil {
		return page, err
	}
	for _, row := range page.Data {
		s.monsterAssets(row)
	}
	return page, nil
}
func (s *Service) Monster(ctx context.Context, id int64) (Object, error) {
	if id <= 0 {
		return nil, ErrNotFound
	}
	row, err := s.Repository.Monster(ctx, id)
	if err != nil {
		return nil, err
	}
	s.monsterAssets(row)
	return row, nil
}
func (s *Service) NPCs(ctx context.Context, q NPCQuery) (Page, error) {
	if !q.Pagination.Valid() || !short(q.Search, 100) || !short(q.Map, 64) || !short(q.Name, 100) || !short(q.DisplayName, 100) || !short(q.OnMap, 64) || !short(q.CurrentMap, 64) {
		return Page{}, ErrQuery
	}
	page, err := s.Repository.NPCs(ctx, q)
	if err != nil {
		return page, err
	}
	for _, row := range page.Data {
		s.npcAssets(row)
	}
	return page, nil
}
func (s *Service) NPCsOnMap(ctx context.Context, name string, gameOnly bool) ([]Object, error) {
	if name == "" || !short(name, 64) {
		return nil, ErrQuery
	}
	rows, err := s.Repository.NPCsOnMap(ctx, name, gameOnly)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		s.npcAssets(row)
	}
	return rows, nil
}
func (s *Service) itemAssets(row Object) {
	id, _ := integer(row["Id"])
	row["icon"] = "/game/items/item-placeholder.bmp"
	row["illustration"] = nil
	if s.Assets.Exists("icon", id) {
		row["icon"] = fmt.Sprintf("/api/game-data/items/%d/icon", id)
	}
	if s.Assets.Exists("illustration", id) {
		row["illustration"] = fmt.Sprintf("/api/game-data/items/%d/illustration", id)
	}
}
func (s *Service) monsterAssets(row Object) {
	id, _ := integer(row["Id"])
	row["image"] = nil
	if s.Assets.Exists("monster", id) {
		row["image"] = fmt.Sprintf("/api/game-data/monsters/%d/image", id)
	}
}
func (s *Service) npcAssets(row Object) {
	id, ok := integer(row["display_sprite_id"])
	row["image"] = nil
	if ok && boolValue(row["image_available"], false) && s.Assets.Exists("npc", id) {
		row["image"] = fmt.Sprintf("/api/game-data/npcs/%d/image", id)
	}
}
