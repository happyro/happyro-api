package httptransport

import (
	"context"
	"encoding/json"
	gameapi "github.com/happyro/happyro-api/internal/game"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/happyro/happyro-api/internal/catalog"
)

type adventureCatalog struct {
	catalog.Repository
	items    catalog.ItemQuery
	npcs     catalog.NPCQuery
	gameOnly bool
}

func (r *adventureCatalog) Items(_ context.Context, q catalog.ItemQuery) (catalog.Page, error) {
	r.items = q
	subtypes := []string{"Dagger", "Sword"}
	return catalog.Page{Data: []catalog.Object{{"Id": int64(501), "Type": "PetEgg"}}, Total: 1, Subtypes: &subtypes}, nil
}
func (r *adventureCatalog) Item(_ context.Context, id int64, scope string) (catalog.Object, error) {
	r.items.Range = scope
	return catalog.Object{"Id": id, "Type": "Healing"}, nil
}
func (r *adventureCatalog) NPCs(_ context.Context, q catalog.NPCQuery) (catalog.Page, error) {
	r.npcs = q
	return catalog.Page{Data: []catalog.Object{}, Total: 0}, nil
}
func (r *adventureCatalog) NPCsOnMap(_ context.Context, _ string, gameOnly bool) ([]catalog.Object, error) {
	r.gameOnly = gameOnly
	return []catalog.Object{}, nil
}
func TestAdventureCatalogEnforcesGameScopeAndAuthenticatedAssets(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "icons"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "icons", "501.png"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	assets := catalog.FilesystemAssets{ItemRoot: root}
	repo := &adventureCatalog{}
	b, _ := newBrowser(t, func(s *Server) {
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}}
		s.PlayerLimits = &testAdventureLimit{}
		s.Catalog = &catalog.Service{Repository: repo, Assets: assets}
		s.CatalogAssets = assets
	})
	get := func(path string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if authenticated {
			r.Header.Set("X-HappyRO-Account-ID", "2000000")
			r.Header.Set("X-HappyRO-Character-ID", "150000")
			r.Header.Set("X-HappyRO-Auth-Token", "game-token")
		}
		w := httptest.NewRecorder()
		b.handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/items", "/items/501", "/items/501/icon", "/items/501/illustration", "/npcs", "/maps", "/maps/prontera/npcs"} {
		expectStatus(t, get("/api/adventure-tools"+path, false), 401)
	}
	w := get("/api/adventure-tools/items?range=all", true)
	expectStatus(t, w, 200)
	if repo.items.Range != "client" || repo.items.Pagination.Size != 30 {
		t.Fatalf("scope/default %+v", repo.items)
	}
	if !strings.Contains(w.Body.String(), `"grantable":false`) || !strings.Contains(w.Body.String(), `"icon":"/api/adventure-tools/items/501/icon"`) {
		t.Fatal(w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"subtypes":["Dagger","Sword"]`) {
		t.Fatalf("item subtype filters missing from response: %s", w.Body.String())
	}
	expectStatus(t, get("/api/adventure-tools/items?perPage=51", true), 422)
	expectStatus(t, get("/api/adventure-tools/items/501?range=server", true), 200)
	if repo.items.Range != "client" {
		t.Fatal("detail scope escaped")
	}
	expectStatus(t, get("/api/adventure-tools/npcs?visibility=all&onMap=prontera&currentMap=geffen", true), 200)
	if !repo.npcs.GameOnly || repo.npcs.OnMap != "prontera" || repo.npcs.CurrentMap != "geffen" || repo.npcs.Pagination.Size != 32 {
		t.Fatalf("NPC filters %+v", repo.npcs)
	}
	expectStatus(t, get("/api/adventure-tools/maps/prontera/npcs?visibility=all", true), 200)
	if !repo.gameOnly {
		t.Fatal("hidden NPCs exposed")
	}
	w = get("/api/adventure-tools/items/501/icon", true)
	expectStatus(t, w, 200)
	if w.Header().Get("Cache-Control") != "private, max-age=86400" || w.Body.String() != "image" {
		t.Fatal("asset cache/body")
	}
	expectStatus(t, get("/api/adventure-tools/items/501/illustration", true), 404)
}

type adventureMapGateway struct {
	gameapi.Gateway
	enabled int64
}

func (g *adventureMapGateway) BattleConfig(context.Context) (map[string]int64, error) {
	return map[string]int64{"navigation_map_channels_enabled": g.enabled}, nil
}
func TestAdventureMapsUseLiveChannelsAndGameOnlyScope(t *testing.T) {
	assets := fstest.MapFS{
		"map-catalog.json":     {Data: []byte(`{"version":"test","entries":[{"map":"town","supported":true,"channel":1,"canonical_map":"town"},{"map":"town2","supported":true,"channel":2,"canonical_map":"town"},{"map":"field","supported":true}]}`)},
		"map-names.zh-CN.json": {Data: []byte(`{}`)},
		"map-order.json":       {Data: []byte(`{"common":["town"]}`)},
	}
	maps, err := catalog.LoadMaps(assets, strings.NewReader("town 1\ntown2 2\nfield 3\nunused 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	gateway := &adventureMapGateway{}
	b, _ := newBrowser(t, func(s *Server) {
		s.GameAuth = &sessionauth.GameAuthentication{Repository: gameIdentity{}}
		s.PlayerLimits = &testAdventureLimit{}
		s.Maps = maps
		s.GameGateway = gateway
	})
	get := func(query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/adventure-tools/maps"+query, nil)
		r.Header.Set("X-HappyRO-Account-ID", "2000000")
		r.Header.Set("X-HappyRO-Character-ID", "150000")
		r.Header.Set("X-HappyRO-Auth-Token", "game-token")
		w := httptest.NewRecorder()
		b.handler.ServeHTTP(w, r)
		return w
	}
	for _, scenario := range []struct {
		enabled int64
		query   string
		total   int
		first   string
	}{
		{0, "?scope=all&currentMap=field", 2, "field"},
		{1, "?onMap=town2", 1, "town2"},
		{0, "?onMap=town2", 1, "town"},
	} {
		gateway.enabled = scenario.enabled
		response := get(scenario.query)
		expectStatus(t, response, 200)
		var page catalog.MapPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != scenario.total || len(page.Data) == 0 || page.Data[0].Map != scenario.first {
			t.Fatalf("map filtering: %+v", page)
		}
	}
	expectStatus(t, get("?perPage=51"), 422)
	expectStatus(t, get("?query="+strings.Repeat("a", 101)), 422)
}
