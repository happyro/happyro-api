package rewards

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/happyro/happyro-api/internal/catalog"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

type absentRecords struct{}

func (absentRecords) Find(context.Context, string) (*MailRecord, error) { return nil, nil }
func (absentRecords) Create(context.Context, string, string, PreparedMail, int64) (MailRecord, error) {
	panic("invalid mail persisted")
}
func (absentRecords) Sent(context.Context, MailRecord, int64, identityapi.User, identityapi.Client) error {
	panic("invalid mail sent")
}

type itemType string

func (i itemType) Item(context.Context, int64, string) (catalog.Object, error) {
	return catalog.Object{"Type": string(i)}, nil
}
func TestMailRejectsUnsafeQuantityAndUTF8WireLengths(t *testing.T) {
	base := ItemRequest{Key: "valid", ItemID: 501, CharacterID: 150000, Amount: 1, Title: "奖励", Message: "邮件正文"}
	cases := []struct {
		name, kind string
		change     func(*ItemRequest)
	}{
		{"empty key", "Healing", func(r *ItemRequest) { r.Key = "" }},
		{"too long key", "Healing", func(r *ItemRequest) { r.Key = strings.Repeat("a", 65) }},
		{"negative id", "Healing", func(r *ItemRequest) { r.CharacterID = -1 }},
		{"zero amount", "Healing", func(r *ItemRequest) { r.Amount = 0 }},
		{"stack overflow", "Healing", func(r *ItemRequest) { r.Amount = 30001 }},
		{"equipment attachment slots", "Armor", func(r *ItemRequest) { r.Amount = 6 }},
		{"pet eggs", "PetEgg", func(r *ItemRequest) { r.Amount = 2 }},
		{"title bytes", "Healing", func(r *ItemRequest) { r.Title = strings.Repeat("中", 14) }},
		{"body bytes", "Healing", func(r *ItemRequest) { r.Message = strings.Repeat("中", 167) }},
		{"empty body", "Healing", func(r *ItemRequest) { r.Message = "   " }},
		{"nul body", "Healing", func(r *ItemRequest) { r.Message = "a\x00b" }},
		{"invalid utf8", "Healing", func(r *ItemRequest) { r.Title = string([]byte{0xff}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := base
			tc.change(&input)
			s := Service{Items: itemType(tc.kind), Records: absentRecords{}}
			_, err := s.MailItem(context.Background(), input, identityapi.User{ID: 1, Username: "admin"}, identityapi.Client{})
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
	for _, value := range []string{strings.Repeat("中", 13), strings.Repeat("a", 39)} {
		if err := MailText(value, "标题", 39, true); err != nil {
			t.Fatal(err)
		}
	}
}
func TestInventoryRejectsUnsupportedBindingAndPetEggBeforeExecution(t *testing.T) {
	for _, tc := range []struct {
		bound bool
		kind  string
	}{{true, "Armor"}, {false, "PetEgg"}} {
		s := Service{Items: itemType(tc.kind)}
		_, err := s.Inventory(context.Background(), ItemRequest{Key: "grant", ItemID: 501, CharacterID: 150000, Amount: 1, Bound: tc.bound}, gameapi.Actor{}, identityapi.Client{})
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Fatalf("expected validation error: %v", err)
		}
	}
}
