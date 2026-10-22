package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jmoiron/sqlx"
)

type ImportResult struct {
	Generation int64 `json:"generation"`
	Items      int   `json:"items"`
	Monsters   int   `json:"monsters"`
	NPCs       int   `json:"npcs"`
	Changed    bool  `json:"changed"`
}
type Publisher interface {
	Publish(context.Context, Bundle) (ImportResult, error)
}
type Importer struct {
	Reader    SnapshotReader
	Publisher Publisher
}

func (s *Importer) Import(ctx context.Context, paths Paths) (ImportResult, error) {
	bundle, err := s.Reader.Read(paths)
	if err != nil {
		return ImportResult{}, err
	}
	return s.Publisher.Publish(ctx, bundle)
}

type SQLPublisher struct{ DB *sqlx.DB }

func (p *SQLPublisher) Publish(ctx context.Context, b Bundle) (ImportResult, error) {
	if len(b.Client.Entries) == 0 || len(b.Server.Entries) == 0 || len(b.Monsters.Entries) == 0 || b.Client.Version == "" || b.Server.Version == "" || b.Monsters.Version != b.Server.Version || b.NPCs.Version == "" {
		return ImportResult{}, errors.New("cannot publish an incomplete catalog bundle")
	}
	tx, err := p.DB.BeginTxx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()
	var current sql.NullInt64
	if err = tx.GetContext(ctx, &current, "SELECT generation_id FROM catalog_state WHERE id=1 FOR UPDATE"); err != nil {
		return ImportResult{}, err
	}
	var previous struct {
		ID       int64 `db:"id"`
		Items    int   `db:"item_count"`
		Monsters int   `db:"monster_count"`
		NPCs     int   `db:"npc_count"`
	}
	err = tx.GetContext(ctx, &previous, "SELECT id,item_count,monster_count,npc_count FROM catalog_generations WHERE content_hash=?", b.Hash())
	if err == nil {
		result := ImportResult{Generation: previous.ID, Items: previous.Items, Monsters: previous.Monsters, NPCs: previous.NPCs, Changed: current.Int64 != previous.ID}
		if result.Changed {
			if err = activate(ctx, tx, result, b); err != nil {
				return result, err
			}
		}
		return result, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ImportResult{}, err
	}
	ids := map[int64]bool{}
	for id := range b.Client.Entries {
		ids[id] = true
	}
	for id := range b.Server.Entries {
		ids[id] = true
	}
	result := ImportResult{Items: len(ids), Monsters: len(b.Monsters.Entries), NPCs: len(b.NPCs.NPCs), Changed: true}
	metadata, err := json.Marshal(Object{"client": Object{"sha256": b.Client.Hash, "source": b.Client.Metadata}, "server": Object{"sha256": b.Server.Hash, "source": b.Server.Metadata}, "monsters": Object{"sha256": b.Monsters.Hash, "source": b.Monsters.Metadata}, "npcs": Object{"sha256": b.NPCs.Hash, "source": b.NPCs.Metadata}})
	if err != nil {
		return result, err
	}
	inserted, err := tx.ExecContext(ctx, `INSERT INTO catalog_generations (content_hash,client_version,server_version,npc_version,metadata,item_count,monster_count,npc_count) VALUES (?,?,?,?,?,?,?,?)`, b.Hash(), b.Client.Version, b.Server.Version, b.NPCs.Version, metadata, result.Items, result.Monsters, result.NPCs)
	if err != nil {
		return result, err
	}
	result.Generation, err = inserted.LastInsertId()
	if err != nil {
		return result, err
	}
	if err = publishItems(ctx, tx, result.Generation, b, ids); err != nil {
		return result, err
	}
	if err = publishMonsters(ctx, tx, result.Generation, b); err != nil {
		return result, err
	}
	if err = publishNPCs(ctx, tx, result.Generation, b.NPCs); err != nil {
		return result, err
	}
	if err = activate(ctx, tx, result, b); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func activate(ctx context.Context, tx *sqlx.Tx, result ImportResult, b Bundle) error {
	if _, err := tx.ExecContext(ctx, "UPDATE catalog_state SET generation_id=? WHERE id=1", result.Generation); err != nil {
		return err
	}
	metadata, err := json.Marshal(Object{"generation": result.Generation, "clientVersion": b.Client.Version, "serverVersion": b.Server.Version, "npcVersion": b.NPCs.Version, "items": result.Items, "monsters": result.Monsters, "npcs": result.NPCs})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs (username,action,user_agent,metadata) VALUES ('console','settings.game_data_updated','console',?)`, metadata)
	return err
}
func insertRows(ctx context.Context, tx *sqlx.Tx, table, columns string, rows [][]any) error {
	for start := 0; start < len(rows); start += 100 {
		batch := rows[start:min(start+100, len(rows))]
		placeholders := []string{}
		args := []any{}
		for _, row := range batch {
			placeholders = append(placeholders, "("+strings.TrimSuffix(strings.Repeat("?,", len(row)), ",")+")")
			args = append(args, row...)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" ("+columns+") VALUES "+strings.Join(placeholders, ","), args...); err != nil {
			return fmt.Errorf("publish %s: %w", table, err)
		}
	}
	return nil
}
func sortedIDs[V any](entries map[int64]V) []int64 {
	ids := make([]int64, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
func publishItems(ctx context.Context, tx *sqlx.Tx, generation int64, b Bundle, ids map[int64]bool) error {
	rows := make([][]any, 0, len(ids))
	for _, id := range sortedIDs(ids) {
		client, server := b.Client.Entries[id], b.Server.Entries[id]
		view := itemView(id, client, server, b.Client.Version, b.Server.Version)
		payload, err := json.Marshal(view)
		if err != nil {
			return err
		}
		names := object(view["names"])
		rows = append(rows, []any{generation, id, text(names["zh-CN"]), text(names["en-US"]), view["AegisName"], view["Type"], view["SubType"], client != nil, server != nil, payload})
	}
	return insertRows(ctx, tx, "catalog_items", "generation_id,item_id,name_zh_cn,name_en_us,aegis_name,item_type,item_subtype,client_exists,server_exists,payload", rows)
}
func publishMonsters(ctx context.Context, tx *sqlx.Tx, generation int64, b Bundle) error {
	monsters, drops := [][]any{}, [][]any{}
	for _, id := range sortedIDs(b.Monsters.Entries) {
		source := b.Monsters.Entries[id]
		view := monsterView(id, source, b.Server.Version)
		names := object(view["names"])
		payload, err := json.Marshal(view)
		if err != nil {
			return err
		}
		monsters = append(monsters, []any{generation, id, text(names["zh-CN"]), text(names["en-US"]), view["AegisName"], view["kind"], view["Race"], view["Element"], view["Size"], view["Level"], payload})
		for field, method := range map[string]string{"Drops": "normal", "MvpDrops": "mvp_reward"} {
			list, _ := source[field].([]any)
			for slot, value := range list {
				drop := object(value)
				name := text(drop["Item"])
				rate := number(drop["Rate"], 0)
				if name == "" || rate <= 0 {
					continue
				}
				drops = append(drops, []any{generation, id, name, method, slot, rate})
			}
		}
	}
	if err := insertRows(ctx, tx, "catalog_monsters", "generation_id,monster_id,name_zh_cn,name_en_us,aegis_name,kind,race,element,size,level,payload", monsters); err != nil {
		return err
	}
	return insertRows(ctx, tx, "catalog_drops", "generation_id,monster_id,item_aegis_name,method,slot,rate", drops)
}
func publishNPCs(ctx context.Context, tx *sqlx.Tx, generation int64, snapshot Snapshot) error {
	rows := make([][]any, 0, len(snapshot.NPCs))
	for _, source := range snapshot.NPCs {
		view := npcView(source)
		navigation := object(view["navigation"])
		payload, err := json.Marshal(view)
		if err != nil {
			return err
		}
		rows = append(rows, []any{generation, view["id"], view["map"], text(view["map_name_zh_cn"]), view["name"], view["source_name"], view["display_name"], text(navigation["name"]), scalarText(navigation["class"]), view["game_visible"], view["catalog_order"], view["x"], view["y"], payload})
	}
	return insertRows(ctx, tx, "catalog_npcs", "generation_id,npc_key,map,map_name,name,source_name,display_name,navigation_name,navigation_class,game_visible,catalog_order,x,y,payload", rows)
}

func scalarText(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case json.Number:
		return value.String()
	case int:
		return fmt.Sprint(value)
	}
	return ""
}
