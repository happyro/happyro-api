package catalog

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/happyro/happyro-api/internal/platform/database"
)

func TestMapsRespectChannelsNamesAliasesAndCurrentMap(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":     {Data: []byte(`{"version":"test","entries":[{"map":"town","name":"城镇","supported":true,"channel":1,"canonical_map":"town","image_map":"town","image_kind":"image"},{"map":"town2","name":"城镇","supported":true,"channel":2,"canonical_map":"town","image_map":"town","image_kind":"image"},{"map":"field","name":"原野","supported":true,"image_map":"field","image_kind":"terrain"}]}`)},
		"map-names.zh-CN.json": {Data: []byte(`{"unused":"其他地图"}`)},
		"map-order.json":       {Data: []byte(`{"common":["town"]}`)},
		"maps/town.png":        {Data: []byte("image")},
		"terrain/field.png":    {Data: []byte("image")},
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
