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
	page     database.Page
	err      error
}

func (r *queryReader) Characters(_ context.Context, q Query) (database.Page, error) {
	r.received = q
	r.calls++
	return r.page, r.err
}
func (*queryReader) Character(context.Context, int64) (database.Row, error) { return nil, nil }

type mapNames map[string]string

func (names mapNames) MapName(code string) *string {
	name, ok := names[code]
	if !ok {
		return nil
	}
	return &name
}

func TestCharactersResolveMapNamesWithoutLosingCodesOrPagination(t *testing.T) {
	reader := &queryReader{page: database.Page{
		Data:  []database.Row{{"last_map": "new_1-1"}, {"last_map": "wolfvill"}, {"last_map": "unlisted"}},
		Total: 43, Current: 2, PageSize: 20,
	}}
	service := &Service{Reader: reader, Maps: mapNames{"new_1-1": "初心者修炼场", "wolfvill": "灰狼村"}}
	page, err := service.Characters(context.Background(), Query{Pagination: database.Pagination{Page: 2, Size: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 43 || page.Current != 2 || page.PageSize != 20 || len(page.Data) != 3 {
		t.Fatalf("pagination changed: %+v", page)
	}
	for i, want := range []string{"初心者修炼场", "灰狼村"} {
		name, ok := page.Data[i]["last_map_name"].(*string)
		if !ok || name == nil || *name != want {
			t.Fatalf("map name: %+v", page.Data[i])
		}
	}
	if name := page.Data[2]["last_map_name"].(*string); name != nil || page.Data[2]["last_map"] != "unlisted" {
		t.Fatalf("unknown map lost: %+v", page.Data[2])
	}
}

func TestCharactersPropagateReadFailure(t *testing.T) {
	want := errors.New("read failed")
	service := &Service{Reader: &queryReader{err: want}}
	_, err := service.Characters(context.Background(), Query{Pagination: database.Pagination{Page: 1, Size: 20}})
	if !errors.Is(err, want) {
		t.Fatalf("read error lost: %v", err)
	}
}

func TestCharacterFilters(t *testing.T) {
	reader := &queryReader{}
	service := &Service{Reader: reader}
	query := Query{Search: "target", OnlineOnly: true, Username: "owner", Name: "测试_角色%", Pagination: database.Pagination{Page: 1, Size: 20}}
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

func TestCharacterSearchRejectsInvalidText(t *testing.T) {
	reader := &queryReader{}
	service := &Service{Reader: reader}
	for _, text := range []string{strings.Repeat("x", 257), string([]byte{0xff})} {
		_, err := service.Characters(context.Background(), Query{Search: text, Pagination: database.Pagination{Page: 1, Size: 20}})
		if !errors.Is(err, ErrFilter) {
			t.Fatalf("invalid search accepted: %v", err)
		}
	}
	if reader.calls != 0 {
		t.Fatal("invalid search reached repository")
	}
}
