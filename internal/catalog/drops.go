package catalog

import (
	"context"
	"github.com/jmoiron/sqlx"
)

func (r *SQLRepository) dropSources(ctx context.Context, generation int64, aegis string) ([]Object, error) {
	result := []Object{}
	if aegis == "" {
		return result, nil
	}
	var rows []struct {
		ID      int64  `db:"monster_id"`
		Chinese string `db:"name_zh_cn"`
		English string `db:"name_en_us"`
		Aegis   string `db:"aegis_name"`
		Kind    string `db:"kind"`
		Level   int64  `db:"level"`
		Method  string `db:"method"`
		Rate    int64  `db:"rate"`
	}
	err := r.DB.SelectContext(ctx, &rows, `SELECT m.monster_id,m.name_zh_cn,m.name_en_us,m.aegis_name,m.kind,m.level,d.method,d.rate
 FROM catalog_drops d JOIN catalog_monsters m ON m.generation_id=d.generation_id AND m.monster_id=d.monster_id
 WHERE d.generation_id=? AND d.item_aegis_name=? ORDER BY m.monster_id,d.method,d.rate DESC`, generation, aegis)
	if err != nil {
		return nil, err
	}
	indices := map[int64]int{}
	for _, row := range rows {
		index, exists := indices[row.ID]
		if !exists {
			index = len(result)
			indices[row.ID] = index
			name := row.Chinese
			if name == "" {
				name = row.English
			}
			if name == "" {
				name = row.Aegis
			}
			result = append(result, Object{"id": row.ID, "name": name, "kind": row.Kind, "level": row.Level, "drops": []Object{}})
		}
		drops := result[index]["drops"].([]Object)
		found := false
		for _, drop := range drops {
			if drop["method"] == row.Method && drop["rate"] == row.Rate {
				drop["count"] = drop["count"].(int) + 1
				found = true
				break
			}
		}
		if !found {
			drops = append(drops, Object{"method": row.Method, "rate": row.Rate, "count": 1})
		}
		result[index]["drops"] = drops
	}
	return result, nil
}
func (r *SQLRepository) localizeDrops(ctx context.Context, generation int64, monster Object) error {
	names := []string{}
	for _, field := range []string{"Drops", "MvpDrops"} {
		list, _ := monster[field].([]any)
		for _, value := range list {
			if name := text(object(value)["Item"]); name != "" {
				names = append(names, name)
			}
		}
	}
	if len(names) == 0 {
		return nil
	}
	query, args, err := sqlx.In("SELECT item_id,aegis_name,name_zh_cn,name_en_us FROM catalog_items WHERE generation_id=? AND aegis_name IN (?)", generation, names)
	if err != nil {
		return err
	}
	var rows []struct {
		ID      int64  `db:"item_id"`
		Aegis   string `db:"aegis_name"`
		Chinese string `db:"name_zh_cn"`
		English string `db:"name_en_us"`
	}
	if err = r.DB.SelectContext(ctx, &rows, query, args...); err != nil {
		return err
	}
	localized := map[string]Object{}
	for _, row := range rows {
		chinese, english := row.Chinese, row.English
		if chinese == "" {
			chinese = row.Aegis
		}
		if english == "" {
			english = row.Aegis
		}
		localized[row.Aegis] = Object{"itemId": row.ID, "names": Object{"zh-CN": chinese, "en-US": english}}
	}
	for _, field := range []string{"Drops", "MvpDrops"} {
		list, _ := monster[field].([]any)
		for _, value := range list {
			drop := object(value)
			if drop == nil {
				continue
			}
			aegis := text(drop["Item"])
			item, ok := localized[aegis]
			if !ok {
				item = Object{"itemId": 0, "names": Object{"zh-CN": aegis, "en-US": aegis}}
			}
			drop["itemId"], drop["names"] = item["itemId"], item["names"]
		}
	}
	return nil
}
