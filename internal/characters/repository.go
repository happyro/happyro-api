package characters

import (
	"context"
	"strings"

	"github.com/happyro/happyro-api/internal/platform/database"
	"github.com/jmoiron/sqlx"
)

type SQLReader struct{ Game *sqlx.DB }

// Escape SQL LIKE metacharacters so names containing '%' or '_' are searched
// literally. The explicit escape character also works under NO_BACKSLASH_ESCAPES.
func search(value string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(value) + "%"
}

const characterSummary = "c.char_id,c.account_id,a.userid AS username,c.name,c.class,c.base_level,c.job_level,c.last_map,c.online,CAST(c.last_login AS CHAR) AS last_login"
const characterDetails = characterSummary + ",c.base_exp,c.job_exp,c.zeny,c.str,c.agi,c.vit,c.`int`,c.dex,c.luk,c.max_hp,c.hp,c.max_sp,c.sp,c.status_point,c.skill_point,c.pow,c.sta,c.wis,c.spl,c.con,c.crt,c.trait_point,c.ap,c.max_ap,c.last_x,c.last_y"
const characterSource = "`char` c LEFT JOIN login a ON a.account_id=c.account_id"

func (r *SQLReader) Characters(ctx context.Context, q Query) (database.Page, error) {
	filters := []string{}
	args := []any{}
	if q.Username != "" {
		filters = append(filters, "a.userid LIKE ? ESCAPE '!'")
		args = append(args, search(q.Username))
	}
	if q.Name != "" {
		filters = append(filters, "c.name LIKE ? ESCAPE '!'")
		args = append(args, search(q.Name))
	}
	where := ""
	if len(filters) > 0 {
		where = " WHERE " + strings.Join(filters, " AND ")
	}
	return database.Paginate(ctx, r.Game, characterSummary, characterSource, where, "c.char_id DESC", args, q.Pagination)
}

func (r *SQLReader) Character(ctx context.Context, id int64) (database.Row, error) {
	rows, err := database.Rows(ctx, r.Game, "SELECT "+characterDetails+" FROM "+characterSource+" WHERE c.char_id=?", id)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rows[0], nil
}
