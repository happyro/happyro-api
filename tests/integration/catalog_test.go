package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/catalog"
	"github.com/happyro/happyro-api/internal/platform/database"
)

func snapshotFile(t *testing.T, directory, name string, payload any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func catalogPaths(t *testing.T) catalog.Paths {
	t.Helper()
	directory := t.TempDir()
	names := func(chinese, english string) map[string]any {
		return map[string]any{"zh-CN": chinese, "en-US": english}
	}
	client := map[string]any{"schema": "item-catalog/v2", "mode": "client-only", "clientSource": map[string]any{"version": "client-test"}, "items": map[string]any{
		"501": map[string]any{"Id": 501, "names": names("客户端盾牌", "Client shield"), "Buy": 1, "identifiedResourceName": "shield", "identifiedDescriptionName": []string{"^FF0000说明", "_", ""}},
		"502": map[string]any{"Id": 502, "names": names("客户端专属", "Client only")},
	}}
	server := map[string]any{"schema": "item-catalog/v2", "mode": "renewal", "englishSource": map[string]any{"revision": "server-test"}, "items": map[string]any{
		"501": map[string]any{"Id": 501, "names": names("服务端盾牌", "Server shield"), "AegisName": "Shield", "Type": "Armor", "Locations": map[string]bool{"Left_Hand": true}, "Buy": 300, "Weight": 100},
		"503": map[string]any{"Id": 503, "names": names("药水", "Potion"), "AegisName": "Potion", "Type": "Healing"},
	}}
	monsters := map[string]any{"schema": "monster-catalog/v1", "serverSource": map[string]any{"revision": "server-test"}, "monsters": map[string]any{
		"1002": map[string]any{"Id": 1002, "names": names("测试首领", "Test boss"), "AegisName": "TEST_BOSS", "Class": "Boss", "Drops": []any{map[string]any{"Item": "Shield", "Rate": 100}, map[string]any{"Item": "Shield", "Rate": 100}}},
		"1003": map[string]any{"Id": 1003, "names": names("测试 MVP", "Test MVP"), "MvpDrops": []any{map[string]any{"Item": "Potion", "Rate": 5000}}},
	}}
	npcs := map[string]any{"schema": "happyro-npc-catalog/v1", "version": "npc-test", "entries": []any{
		map[string]any{"id": "npc-one", "map": "prontera", "name": "kafra", "source_name": "카프라", "display_name": "卡普拉", "game_visible": true, "catalog_order": 1, "navigation": map[string]any{"name": "카프라", "class": 10}},
		map[string]any{"id": "npc-hidden", "map": "prontera", "name": "hidden", "source_name": "hidden", "display_name": "隐藏 NPC", "game_visible": false, "catalog_order": 2},
	}}
	return catalog.Paths{Client: snapshotFile(t, directory, "client.json", client), Server: snapshotFile(t, directory, "server.json", server), Monsters: snapshotFile(t, directory, "monsters.json", monsters), NPCs: snapshotFile(t, directory, "npcs.json", npcs)}
}
func TestCatalogImportMergesSourcesAndIndexesDropsAndAliases(t *testing.T) {
	db := testDatabase(t)
	paths := catalogPaths(t)
	importer := catalog.Importer{Reader: catalog.JSONReader{}, Publisher: &catalog.SQLPublisher{DB: db}}
	result, err := importer.Import(context.Background(), paths)
	if err != nil || result.Items != 3 || result.Monsters != 2 || result.NPCs != 2 || !result.Changed {
		t.Fatalf("import: %+v %v", result, err)
	}
	repeat, err := importer.Import(context.Background(), paths)
	if err != nil || repeat.Changed || repeat.Generation != result.Generation {
		t.Fatalf("unchanged import: %+v %v", repeat, err)
	}
	history, err := (&audit.SQLStore{DB: db}).History(context.Background(), "settings.game_data_updated", database.Pagination{Page: 1, Size: 20})
	if err != nil || history.Meta.Total != 1 || len(history.Data) != 1 {
		t.Fatalf("import history: %+v %v", history, err)
	}
	if history.Data[0]["metadata"].(map[string]any)["generation"] != json.Number("1") {
		t.Fatalf("history metadata: %+v", history.Data)
	}
	repo := &catalog.SQLRepository{DB: db, Aliases: map[string]string{"Kafra": "카프라"}}
	service := catalog.Service{Repository: repo, Assets: catalog.FilesystemAssets{}}
	item, err := service.Item(context.Background(), 501, "client")
	if err != nil {
		t.Fatal(err)
	}
	if item["Buy"] != json.Number("300") || item["SubType"] != "Shield" || item["source"] != "both" {
		t.Fatalf("wrong merged fields: %+v", item)
	}
	if item["names"].(map[string]any)["zh-CN"] != "客户端盾牌" {
		t.Fatal("server name replaced client name")
	}
	description := item["description"].([]any)
	if len(description) != 1 || description[0] != "说明" {
		t.Fatalf("description: %v", description)
	}
	drops := item["dropSources"].([]catalog.Object)
	if len(drops) != 1 || drops[0]["kind"] != "mini" || drops[0]["drops"].([]catalog.Object)[0]["count"] != 2 {
		t.Fatalf("drop index: %+v", drops)
	}
	for _, scope := range []string{"client", "server", "all"} {
		page, err := service.Items(context.Background(), catalog.ItemQuery{Range: scope, Pagination: database.Pagination{Page: 1, Size: 20}})
		expected := int64(2)
		if scope == "all" {
			expected = 3
		}
		if err != nil || page.Total != expected {
			t.Fatalf("range=%s total=%d err=%v", scope, page.Total, err)
		}
	}
	page, err := service.Items(context.Background(), catalog.ItemQuery{Range: "all", DropKind: "mvp", Pagination: database.Pagination{Page: 1, Size: 20}})
	if err != nil || page.Total != 1 || page.Data[0]["Id"] != json.Number("503") {
		t.Fatalf("MVP drops: %+v %v", page, err)
	}
	monster, err := service.Monster(context.Background(), 1002)
	if err != nil {
		t.Fatal(err)
	}
	drop := monster["Drops"].([]any)[0].(map[string]any)
	if drop["itemId"] != int64(501) || drop["names"].(catalog.Object)["zh-CN"] != "客户端盾牌" {
		t.Fatalf("localized drop: %+v", drop)
	}
	npcs, err := service.NPCs(context.Background(), catalog.NPCQuery{Search: "Kafra", GameOnly: true, Pagination: database.Pagination{Page: 1, Size: 20}})
	if err != nil || npcs.Total != 1 || npcs.Data[0]["id"] != "npc-one" {
		t.Fatalf("NPC alias: %+v %v", npcs, err)
	}
	onMap, err := service.NPCsOnMap(context.Background(), " PRONTERA.GAT ", true)
	if err != nil || len(onMap) != 1 {
		t.Fatalf("NPC map: %+v %v", onMap, err)
	}
	versions, err := repo.Versions(context.Background())
	if err != nil || versions.Client != "client-test" || versions.Server != "server-test" {
		t.Fatalf("versions: %+v %v", versions, err)
	}
}

func TestFailedCatalogPublicationLeavesPreviousGenerationIntact(t *testing.T) {
	db := testDatabase(t)
	paths := catalogPaths(t)
	reader := catalog.JSONReader{}
	bundle, err := reader.Read(paths)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &catalog.SQLPublisher{DB: db}
	first, err := publisher.Publish(context.Background(), bundle)
	if err != nil {
		t.Fatal(err)
	}
	// Force a SQL constraint violation after generation metadata and item rows
	// have already been inserted. Every table and the active pointer must roll back.
	bundle.NPCs.Hash = "changed"
	bundle.NPCs.NPCs = append(bundle.NPCs.NPCs, bundle.NPCs.NPCs[0])
	if _, err = publisher.Publish(context.Background(), bundle); err == nil {
		t.Fatal("duplicate NPC publication succeeded")
	}
	var active int64
	var generations int
	if err = db.Get(&active, "SELECT generation_id FROM catalog_state WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err = db.Get(&generations, "SELECT COUNT(*) FROM catalog_generations"); err != nil {
		t.Fatal(err)
	}
	if active != first.Generation || generations != 1 {
		t.Fatalf("partial publication: active=%d generations=%d", active, generations)
	}
}

func TestCatalogReaderRejectsMixedRevisionsAndDuplicateNPCKeys(t *testing.T) {
	for _, scenario := range []string{"revision", "duplicate-npc"} {
		t.Run(scenario, func(t *testing.T) {
			paths := catalogPaths(t)
			path := paths.Monsters
			if scenario == "duplicate-npc" {
				path = paths.NPCs
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err = json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			if scenario == "revision" {
				doc["serverSource"].(map[string]any)["revision"] = "different"
			} else {
				entries := doc["entries"].([]any)
				doc["entries"] = append(entries, entries[0])
			}
			raw, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = (catalog.JSONReader{}).Read(paths); err == nil {
				t.Fatal("invalid bundle accepted")
			}
		})
	}
}
