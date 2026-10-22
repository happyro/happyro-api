package characters

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/happyro/happyro-api/internal/platform/database"
)

type queryReader struct {
	received Query
	calls    int
}

func (r *queryReader) Characters(_ context.Context, q Query) (database.Page, error) {
	r.received = q
	r.calls++
	return database.Page{}, nil
}
func (*queryReader) Character(context.Context, int64) (database.Row, error) { return nil, nil }

func TestCharacterFilters(t *testing.T) {
	reader := &queryReader{}
	service := &Service{Reader: reader}
	query := Query{Username: "owner", Name: "测试_角色%", Pagination: database.Pagination{Page: 1, Size: 20}}
	if _, err := service.Characters(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	if reader.received != query {
		t.Fatalf("filters lost: %+v", reader.received)
	}
	for _, name := range []string{strings.Repeat("x", 257), string([]byte{0xff})} {
		query.Name = name
		if _, err := service.Characters(context.Background(), query); !errors.Is(err, ErrFilter) {
			t.Fatalf("invalid name accepted: %v", err)
		}
	}
	if reader.calls != 1 {
		t.Fatal("invalid filters reached the repository")
	}
}
