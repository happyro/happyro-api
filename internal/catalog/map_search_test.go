package catalog

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/happyro/happyro-api/internal/platform/database"
)

func TestMapSearchUsesDisplayedNamesRankingAndChannels(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":              {Data: []byte(`{"entries":[{"map":"prontera","supported":true},{"map":"prt_fild08","supported":true,"channel":1,"canonical_map":"prt_fild08"},{"map":"prt_fild08a","supported":true,"channel":2,"canonical_map":"prt_fild08"},{"map":"gate","supported":true},{"map":"gate_prefix","supported":true},{"map":"gate_contains","supported":true}]}`)},
		"map-names.zh-CN.json":          {Data: []byte(`{"prontera":"普隆德拉","prt_fild08":"普隆德拉南门","prt_fild08a":"普隆德拉南门","gate":"南门","gate_prefix":"南门训练区","gate_contains":"另一座南门"}`)},
		"map-search-aliases.zh-CN.json": {Data: []byte(`{"prontera":["卢恩·米德加兹王国 首都普隆德拉"],"prt_fild08":["南门","普隆德拉野外"],"prt_fild08a":["南门","普隆德拉野外"]}`)},
		"map-order.json":                {Data: []byte(`{"common":["gate_contains","gate_prefix","prontera","prt_fild08"]}`)},
	}
	maps, err := LoadMaps(assets, strings.NewReader("prontera 1\nprt_fild08 2\nprt_fild08a 3\ngate 4\ngate_prefix 5\ngate_contains 6\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, adventure := range []bool{false, true} {
		q := MapQuery{GameOnly: true, AdventureOrder: adventure, Search: " 南门 ", Pagination: database.Pagination{Page: 1, Size: 20}}
		page, err := maps.Maps(q)
		if err != nil || page.Total != 4 {
			t.Fatalf("search: %+v %v", page, err)
		}
		for i, key := range []string{"gate", "gate_prefix", "gate_contains", "prt_fild08"} {
			if page.Data[i].Map != key {
				t.Fatalf("ranking: %+v", page.Data)
			}
		}
		q.Pagination.Size = 1
		q.Pagination.Page = 2
		page, err = maps.Maps(q)
		if err != nil || page.Total != 4 || len(page.Data) != 1 || page.Data[0].Map != "gate_prefix" {
			t.Fatalf("pagination: %+v %v", page, err)
		}
		q.Pagination = database.Pagination{Page: 1, Size: 20}
		q.ChannelsEnabled = true
		page, err = maps.Maps(q)
		if err != nil || page.Total != 5 {
			t.Fatalf("channels: %+v %v", page, err)
		}
		seen := map[string]bool{}
		for _, row := range page.Data {
			if seen[row.Map] {
				t.Fatal("duplicate map", row.Map)
			}
			seen[row.Map] = true
		}
		for _, query := range []string{"卢恩·米德加兹王国 首都普隆德拉", "首都"} {
			q.Search = query
			page, err = maps.Maps(q)
			if err != nil || page.Total != 0 {
				t.Fatalf("non-display search %q: %+v %v", query, page, err)
			}
		}
	}
}

func TestMapCodeSearchRanksAndPreservesVisibility(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":              {Data: []byte(`{"entries":[{"map":"town","supported":true},{"map":"town_more","supported":true},{"map":"x_town","supported":true},{"map":"town_hidden","supported":true,"player_hidden":true},{"map":"town_channel","supported":true,"channel":2}]}`)},
		"map-names.zh-CN.json":          {Data: []byte(`{"town":"丙","town_more":"乙","x_town":"甲"}`)},
		"map-search-aliases.zh-CN.json": {Data: []byte(`{}`)},
		"map-order.json":                {Data: []byte(`{"common":["x_town"]}`)},
	}
	maps, err := LoadMaps(assets, strings.NewReader("town 1\ntown_more 2\nx_town 3\ntown_hidden 4\ntown_channel 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, adventure := range []bool{false, true} {
		q := MapQuery{GameOnly: true, AdventureOrder: adventure, Search: " TOWN ", Pagination: database.Pagination{Page: 1, Size: 20}}
		page, err := maps.Maps(q)
		if err != nil || page.Total != 3 {
			t.Fatalf("search: %+v %v", page, err)
		}
		for i, id := range []string{"town", "town_more", "x_town"} {
			if page.Data[i].Map != id {
				t.Fatalf("ranking: %+v", page)
			}
		}
		q.Search = "TOWN_HIDDEN"
		page, err = maps.Maps(q)
		if err != nil || page.Total != 0 {
			t.Fatalf("hidden: %+v %v", page, err)
		}
		q.GameOnly = false
		page, err = maps.Maps(q)
		if err != nil || page.Total != 1 || !page.Data[0].PlayerHidden {
			t.Fatalf("all maps: %+v %v", page, err)
		}
		q.GameOnly = true
		q.Search = "TOWN_CHANNEL"
		page, err = maps.Maps(q)
		if err != nil || page.Total != 0 {
			t.Fatalf("channel off: %+v %v", page, err)
		}
		q.ChannelsEnabled = true
		page, err = maps.Maps(q)
		if err != nil || page.Total != 1 {
			t.Fatalf("channel on: %+v %v", page, err)
		}
	}
}
