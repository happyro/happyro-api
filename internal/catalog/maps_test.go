package catalog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/happyro/happyro-api/internal/platform/database"
)

func TestMapsRespectChannelsNamesAliasesAndCurrentMap(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":              {Data: []byte(`{"version":"test","entries":[{"map":"town","name":"城镇","supported":true,"channel":1,"canonical_map":"town","image_map":"town","image_kind":"image"},{"map":"town2","name":"城镇","supported":true,"channel":2,"canonical_map":"town","image_map":"town","image_kind":"image"},{"map":"field","name":"原野","supported":true,"image_map":"field","image_kind":"terrain"}]}`)},
		"map-names.zh-CN.json":          {Data: []byte(`{"unused":"其他地图"}`)},
		"map-search-aliases.zh-CN.json": {Data: []byte(`{}`)},
		"map-order.json":                {Data: []byte(`{"common":["town"]}`)},
		"maps/town.png":                 {Data: []byte("image")},
		"terrain/field.png":             {Data: []byte("image")},
	}
	catalog, err := LoadMaps(assets, strings.NewReader("// index\ntown 1\ntown2 2\nfield 3\nunused 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	q := MapQuery{GameOnly: true, Pagination: database.Pagination{Page: 1, Size: 20}, CurrentMap: "field"}
	page, err := catalog.Maps(q)
	if err != nil || page.Total != 2 || page.Data[0].Map != "field" {
		t.Fatalf("wrong visible/current maps: %+v %v", page, err)
	}
	q.ChannelsEnabled = true
	page, err = catalog.Maps(q)
	if err != nil || page.Total != 3 {
		t.Fatalf("channels hidden: %+v %v", page, err)
	}
	for _, row := range page.Data {
		if row.Map == "town2" && (row.Name == nil || *row.Name != "城镇 · 频道 2") {
			t.Fatalf("wrong channel label: %+v", row)
		}
	}
	path, err := catalog.MapImage("town2")
	if err != nil || path != "maps/town.png" {
		t.Fatalf("image alias: %s %v", path, err)
	}
	if _, err = catalog.MapImage("../outside"); err == nil {
		t.Fatal("unsafe image path accepted")
	}
	q.ChannelsEnabled = false
	q.OnMap = "town2"
	q.CurrentMap = ""
	page, err = catalog.Maps(q)
	if err != nil || page.Total != 1 || page.Data[0].Map != "town" {
		t.Fatalf("canonical scope: %+v %v", page, err)
	}
}

func TestMapsPreferCurrentLocalizedNamesOverStaleCatalogNames(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":              {Data: []byte(`{"version":"test","entries":[{"map":"new_1-1","name":"训练场","supported":true}]}`)},
		"map-names.zh-CN.json":          {Data: []byte(`{"new_1-1":"初心者修炼场"}`)},
		"map-search-aliases.zh-CN.json": {Data: []byte(`{}`)},
		"map-order.json":                {Data: []byte(`{"common":[]}`)},
	}
	maps, err := LoadMaps(assets, strings.NewReader("new_1-1 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := maps.Maps(MapQuery{Search: "初心者修炼场", Pagination: database.Pagination{Page: 1, Size: 20}})
	if err != nil || page.Total != 1 {
		t.Fatalf("localized search failed: %+v %v", page, err)
	}
	if page.Data[0].Name == nil || *page.Data[0].Name != "初心者修炼场" {
		t.Fatalf("stale display name: %+v", page.Data[0])
	}
}

func TestResourceOnlyMapsStayAvailableToAdminButNotPlayers(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":              {Data: []byte(`{"entries":[{"map":"new_event","supported":true,"player_hidden":true},{"map":"pprontera","supported":true,"player_hidden":true},{"map":"alde_gld","supported":true}]}`)},
		"map-names.zh-CN.json":          {Data: []byte(`{"alde_gld":"艾尔帕兰 露依纳"}`)},
		"map-search-aliases.zh-CN.json": {Data: []byte(`{"alde_gld":["艾尔帕兰卫星都市 露依纳"]}`)},
		"map-order.json":                {Data: []byte(`{"common":[]}`)},
	}
	catalog, err := LoadMaps(assets, strings.NewReader("new_event 1\npprontera 2\nalde_gld 3\n"))
	if err != nil {
		t.Fatal(err)
	}
	q := MapQuery{GameOnly: true, Pagination: database.Pagination{Page: 1, Size: 20}}
	page, err := catalog.Maps(q)
	if err != nil || page.Total != 1 || page.Data[0].Map != "alde_gld" {
		t.Fatalf("player list: %+v %v", page, err)
	}
	for _, id := range []string{"new_event", "pprontera"} {
		q.Search = id
		q.CurrentMap = id
		q.OnMap = id
		page, err = catalog.Maps(q)
		if err != nil || page.Total != 0 {
			t.Fatalf("hidden map leaked: %+v %v", page, err)
		}
	}
	q.Search = "艾尔帕兰"
	q.CurrentMap = ""
	q.OnMap = ""
	page, err = catalog.Maps(q)
	if err != nil || page.Total != 1 || *page.Data[0].Name != "艾尔帕兰 露依纳" {
		t.Fatalf("display-name search: %+v %v", page, err)
	}
	q.GameOnly = false
	q.Search = ""
	page, err = catalog.Maps(q)
	if err != nil || page.Total != 3 {
		t.Fatalf("admin lost resources: %+v %v", page, err)
	}
	for _, row := range page.Data {
		if !row.Supported {
			t.Fatalf("resource support changed: %+v", row)
		}
	}
}

func TestNonGameplayMapsAreHiddenButRemainInspectable(t *testing.T) {
	data, err := os.ReadFile("../../resources/game-data/world/map-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Entries []MapEntry `json:"entries"`
	}
	if err = json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	var index strings.Builder
	for _, entry := range catalog.Entries {
		fmt.Fprintf(&index, "%s %d\n", entry.Map, entry.ID)
	}
	maps, err := LoadMaps(os.DirFS("../../resources/game-data/world"), strings.NewReader(index.String()))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"new_zone01", "new_zone02", "new_zone03", "new_zone04", "alde_tt02", "ordeal_a00", "ordeal_a02", "job_sword1", "quiz_01", "que_job02", "que_job03"} {
		for _, channels := range []bool{false, true} {
			page, err := maps.Maps(MapQuery{GameOnly: true, ChannelsEnabled: channels, Map: id, Pagination: database.Pagination{Page: 1, Size: 20}})
			if err != nil || page.Total != 0 {
				t.Fatalf("resource map visible: %s %+v %v", id, page, err)
			}
		}
		page, err := maps.Maps(MapQuery{Map: id, Pagination: database.Pagination{Page: 1, Size: 20}})
		if err != nil || page.Total != 1 || !page.Data[0].PlayerHidden || !page.Data[0].Supported {
			t.Fatalf("admin resource missing: %s %+v %v", id, page, err)
		}
		if maps.MapName(id) == nil {
			t.Fatalf("position label lost: %s", id)
		}
	}
	for _, playable := range []string{"new_1-1", "new_1-2", "new_1-3", "new_1-4", "turbo_n_1", "ordeal_1-1", "ordeal_1-2", "airport", "lhz_airport", "y_airport"} {
		page, err := maps.Maps(MapQuery{GameOnly: true, Map: playable, Pagination: database.Pagination{Page: 1, Size: 20}})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range page.Data {
			if row.Map == playable && !row.PlayerHidden {
				found = true
			}
		}
		if !found {
			t.Fatalf("playable map lost: %s %+v", playable, page)
		}
	}
}
