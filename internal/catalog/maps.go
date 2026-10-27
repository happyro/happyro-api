package catalog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/happyro/happyro-api/internal/platform/database"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

var mapPattern = regexp.MustCompile(`^[a-z0-9_@-]+$`)
var arenaPattern = regexp.MustCompile(`^(pvp_|guild_vs|bat_|schg_|teg_|gvg_)`)
var instancePattern = regexp.MustCompile(`^([12]@|e_|dali|ver_eju|glast_01)`)
var dungeonPattern = regexp.MustCompile(`(_in\d*|_dun\d*|dun\d*|_q\d*|_room|_boss)$`)

type MapEntry struct {
	PlayerHidden bool    `json:"player_hidden"`
	ID           int     `json:"id"`
	Map          string  `json:"map"`
	Name         *string `json:"name"`
	Supported    bool    `json:"supported"`
	Channel      *int    `json:"channel"`
	CanonicalMap string  `json:"canonical_map"`
	ImageMap     string  `json:"image_map"`
	ImageKind    *string `json:"image_kind"`
}
type Map struct {
	PlayerHidden bool    `json:"player_hidden"`
	ID           int     `json:"id"`
	Map          string  `json:"map"`
	Name         *string `json:"name_zh_cn"`
	Supported    bool    `json:"supported"`
	Channel      *int    `json:"channel"`
	ImageKind    *string `json:"image_kind"`
	Image        *string `json:"image"`
}
type MapQuery struct {
	GameOnly, ChannelsEnabled, AdventureOrder bool
	Map, Name, Search, OnMap, CurrentMap      string
	Pagination                                database.Pagination
}
type MapPage struct {
	Data  []Map `json:"data"`
	Total int   `json:"total"`
}
type Maps struct {
	entries map[string]MapEntry
	names   map[string]string
	order   map[string]int
	maps    []Map
}

func LoadMaps(assets fs.FS, index io.Reader) (*Maps, error) {
	var data struct {
		Version string     `json:"version"`
		Entries []MapEntry `json:"entries"`
	}
	var names map[string]string
	var order struct {
		Common []string `json:"common"`
	}
	for name, target := range map[string]any{"map-catalog.json": &data, "map-names.zh-CN.json": &names, "map-order.json": &order} {
		raw, err := fs.ReadFile(assets, name)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, target); err != nil {
			return nil, fmt.Errorf("read world %s: %w", name, err)
		}
	}
	catalog := &Maps{entries: map[string]MapEntry{}, names: names, order: map[string]int{}, maps: []Map{}}
	for _, entry := range data.Entries {
		catalog.entries[entry.Map] = entry
		if catalog.names[entry.Map] == "" && entry.Name != nil {
			catalog.names[entry.Map] = *entry.Name
		}
	}
	for i, name := range order.Common {
		catalog.order[name] = i
	}
	scanner := bufio.NewScanner(index)
	lastID := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		name := fields[0]
		if !mapPattern.MatchString(name) {
			continue
		}
		lastID++
		if len(fields) > 1 {
			value, err := strconv.Atoi(fields[1])
			if err != nil {
				return nil, fmt.Errorf("invalid map index for %s", name)
			}
			lastID = value
		}
		entry := catalog.entries[name]
		row := Map{PlayerHidden: entry.PlayerHidden, ID: lastID, Map: name, Name: catalog.MapName(name), Supported: entry.Supported, Channel: entry.Channel, ImageKind: entry.ImageKind}
		imagePath, _ := catalog.MapImage(name)
		if _, err := fs.Stat(assets, imagePath); err == nil {
			url := "/api/game-data/maps/" + name + "/image?v=" + data.Version
			row.Image = &url
		}
		catalog.maps = append(catalog.maps, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return catalog, nil
}
func (c *Maps) MapName(name string) *string {
	value, ok := c.names[name]
	if !ok {
		return nil
	}
	return &value
}
func (c *Maps) canonical(name string, channels bool) string {
	name = strings.ToLower(name)
	if !channels {
		if entry, ok := c.entries[name]; ok && entry.CanonicalMap != "" {
			return entry.CanonicalMap
		}
	}
	return name
}
func (c *Maps) MapImage(name string) (string, error) {
	if !mapPattern.MatchString(name) {
		return "", fmt.Errorf("invalid map")
	}
	entry := c.entries[name]
	folder := "maps"
	if entry.ImageKind != nil && *entry.ImageKind == "terrain" {
		folder = "terrain"
	}
	image := name
	if entry.ImageMap != "" {
		image = entry.ImageMap
	}
	if !mapPattern.MatchString(image) {
		return "", fmt.Errorf("invalid image alias")
	}
	return folder + "/" + image + ".png", nil
}
func contains(text, query string) bool {
	return query == "" || strings.Contains(strings.ToLower(text), strings.ToLower(query))
}
func nameOf(row Map) string {
	if row.Name == nil {
		return row.Map
	}
	return *row.Name
}

func (c *Maps) Maps(q MapQuery) (MapPage, error) {
	if !q.Pagination.Valid() {
		return MapPage{}, fmt.Errorf("invalid pagination")
	}
	rows := []Map{}
	onMap, currentMap := c.canonical(q.OnMap, q.ChannelsEnabled), c.canonical(q.CurrentMap, q.ChannelsEnabled)
	for _, source := range c.maps {
		if q.GameOnly && (!source.Supported || c.entries[source.Map].PlayerHidden || (!q.ChannelsEnabled && source.Channel != nil && *source.Channel != 1)) {
			continue
		}
		row := source
		if q.ChannelsEnabled && row.Channel != nil && row.Name != nil {
			value := fmt.Sprintf("%s · 频道 %d", *row.Name, *row.Channel)
			row.Name = &value
		}
		name := ""
		if row.Name != nil {
			name = *row.Name
		}
		if !contains(row.Map, q.Map) || !contains(name, q.Name) || (onMap != "" && row.Map != onMap) || !c.matchesSearch(row, q.Search) {
			continue
		}
		rows = append(rows, row)
	}
	collation := collate.New(language.SimplifiedChinese)
	slices.SortStableFunc(rows, func(a, b Map) int {
		left, right := []int{c.commonRank(a.Map)}, []int{c.commonRank(b.Map)}
		if q.AdventureOrder {
			left, right = c.adventureRank(a, q.Search), c.adventureRank(b, q.Search)
		} else if strings.TrimSpace(q.Search) != "" {
			left, right = append([]int{c.searchRank(a, q.Search)}, left...), append([]int{c.searchRank(b, q.Search)}, right...)
		}
		if compared := slices.Compare(left, right); compared != 0 {
			return compared
		}
		if compared := collation.CompareString(nameOf(a), nameOf(b)); compared != 0 {
			return compared
		}
		return strings.Compare(a.Map, b.Map)
	})
	if currentMap != "" && q.Search == "" {
		for i, row := range rows {
			if row.Map == currentMap {
				copy(rows[1:i+1], rows[:i])
				rows[0] = row
				break
			}
		}
	}
	start := min(q.Pagination.Offset(), len(rows))
	end := min(start+q.Pagination.Size, len(rows))
	return MapPage{Data: rows[start:end], Total: len(rows)}, nil
}
func (c *Maps) commonRank(name string) int {
	if rank, ok := c.order[name]; ok {
		return rank
	}
	return len(c.order)
}
func (c *Maps) adventureRank(row Map, query string) []int {
	match := c.searchRank(row, query)
	name := strings.ToLower(nameOf(row))
	kind := 2
	rank, common := c.order[row.Map]
	switch {
	case common:
		kind = 1
	case arenaPattern.MatchString(row.Map) || strings.Contains(name, "pvp") || strings.Contains(name, "对战") || strings.Contains(name, "竞技场"):
		kind = 5
	case instancePattern.MatchString(row.Map):
		kind = 4
	case dungeonPattern.MatchString(row.Map):
		kind = 3
	}
	image := 1
	if row.ImageKind != nil && *row.ImageKind == "image" {
		image = 0
	}
	return []int{match, image, kind, rank}
}
