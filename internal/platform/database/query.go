package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
)

// Row is a database projection, never an entire persisted model. Callers must
// name the columns they expose, especially for login and session tables.
type Row map[string]any

type Page struct {
	Data     []Row `json:"data"`
	Total    int64 `json:"total"`
	Current  int   `json:"current"`
	PageSize int   `json:"pageSize"`
}

type Pagination struct{ Page, Size int }

func (p Pagination) Valid() bool {
	return p.Page >= 1 && p.Page <= 1000000 && p.Size >= 1 && p.Size <= 100
}
func (p Pagination) Offset() int { return (p.Page - 1) * p.Size }

func Rows(ctx context.Context, db sqlx.QueryerContext, query string, args ...any) ([]Row, error) {
	rows, err := db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	result := []Row{}
	for rows.Next() {
		row := Row{}
		if err = rows.MapScan(row); err != nil {
			return nil, err
		}
		for _, column := range types {
			value, ok := row[column.Name()].([]byte)
			if !ok {
				continue
			}
			text := string(value)
			switch strings.ToUpper(column.DatabaseTypeName()) {
			case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "UNSIGNED TINYINT", "UNSIGNED SMALLINT", "UNSIGNED MEDIUMINT", "UNSIGNED INT", "UNSIGNED BIGINT":
				if _, err := strconv.ParseInt(text, 10, 64); err != nil {
					if _, err := strconv.ParseUint(text, 10, 64); err != nil {
						return nil, fmt.Errorf("invalid numeric projection for %s", column.Name())
					}
				}
				row[column.Name()] = json.Number(text)
			case "DECIMAL", "DOUBLE", "FLOAT":
				if _, err := strconv.ParseFloat(text, 64); err != nil {
					return nil, fmt.Errorf("invalid decimal projection for %s", column.Name())
				}
				row[column.Name()] = json.Number(text)
			default:
				row[column.Name()] = text
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func Paginate(ctx context.Context, db *sqlx.DB, columns, from, where, order string, args []any, p Pagination) (Page, error) {
	result := Page{Data: []Row{}, Current: p.Page, PageSize: p.Size}
	if !p.Valid() {
		return result, fmt.Errorf("invalid pagination")
	}
	// Use one read transaction. Snapshot isolation applies to InnoDB tables;
	// existing MyISAM game tables can still change between the count and page.
	tx, err := db.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.GetContext(ctx, &result.Total, "SELECT COUNT(*) FROM "+from+where, args...); err != nil {
		return result, err
	}
	bound := append(append([]any{}, args...), p.Size, p.Offset())
	result.Data, err = Rows(ctx, tx, "SELECT "+columns+" FROM "+from+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", bound...)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
