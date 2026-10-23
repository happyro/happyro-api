package settings

import (
	"context"
	"errors"
	gameapi "github.com/happyro/happyro-api/internal/game"
	"strings"
	"testing"
)

type settleSession struct {
	RevisionSession
	status string
	fail   bool
}

func (s *settleSession) Transition(_ context.Context, _, _, to string, _ *string) error {
	if s.fail {
		return errors.New("database unavailable")
	}
	s.status = to
	return nil
}

type settleFile struct {
	content string
	fail    bool
}

func (f *settleFile) Read() (string, error) { return f.content, nil }
func (f *settleFile) Publish(v string) error {
	if f.fail {
		return errors.New("disk unavailable")
	}
	f.content = v
	return nil
}

type settleGateway struct {
	SettingsGateway
	state                                                gameapi.SettingsState
	unavailable, fenceFailure, lostResponse, commandWins bool
	calls                                                int
}

func (g *settleGateway) SettingsState(context.Context) (gameapi.SettingsState, error) {
	if g.unavailable {
		return gameapi.SettingsState{}, errors.New("map unavailable")
	}
	return g.state, nil
}
func (g *settleGateway) FenceSettings(context.Context, string, uint64) error {
	g.calls++
	if g.fenceFailure {
		return errors.New("fence unavailable")
	}
	g.state.Revision++
	g.state.LastStatus = "fenced"
	if g.commandWins {
		g.state.Values["base_exp_rate"] = 250
		g.state.LastStatus = "succeeded"
	}
	if g.lostResponse {
		return errors.New("response lost")
	}
	return nil
}
func TestSettleAbandonedPublication(t *testing.T) {
	for _, mode := range []string{"restore", "lost fence response", "command wins", "already applied", "new process", "unavailable", "fence fails", "external edit", "file fails", "database fails", "running"} {
		t.Run(mode, func(t *testing.T) {
			instance := "map-one"
			revision := uint64(4)
			desired, _ := Render("// original\n", map[string]int64{"base_exp_rate": 250})
			row := Revision{ID: "revision", Status: "published", Previous: "// original\n", Desired: desired, Changes: []byte(`{"base_exp_rate":250}`), MapInstance: &instance, MapRevision: &revision}
			file := &settleFile{content: desired}
			gateway := &settleGateway{state: gameapi.SettingsState{InstanceID: instance, Revision: revision, Values: map[string]int64{"base_exp_rate": 100}, LastStatus: "idle"}}
			session := &settleSession{}
			want := "failed"
			switch mode {
			case "lost fence response":
				gateway.lostResponse = true
			case "command wins":
				gateway.commandWins = true
				want = "applied"
			case "already applied":
				gateway.state.Revision++
				gateway.state.Values["base_exp_rate"] = 250
				want = "applied"
			case "new process":
				gateway.state.InstanceID = "map-two"
			case "unavailable":
				gateway.unavailable = true
				want = ""
			case "fence fails":
				gateway.fenceFailure = true
				want = ""
			case "external edit":
				file.content = "// operator edit\n"
				want = ""
			case "file fails":
				file.fail = true
				want = ""
			case "database fails":
				session.fail = true
				want = ""
			case "running":
				gateway.state.Revision++
				gateway.state.LastStatus = "running"
				want = ""
			}
			service := Service{Gateway: gateway, File: file}
			err := service.settle(context.Background(), session, &row)
			if session.status != want || (err != nil) != (want == "") {
				t.Fatalf("status=%s want=%s err=%v", session.status, want, err)
			}
			if want == "failed" && !strings.Contains(file.content, "base_exp_rate: 100") {
				t.Fatal("did not retain original effective setting")
			}
			if want == "applied" && file.content != desired {
				t.Fatal("lost applied configuration")
			}
			if mode == "external edit" && file.content != "// operator edit\n" {
				t.Fatal("overwrote operator edit")
			}
			if mode == "database fails" {
				session.fail = false
				if err = service.settle(context.Background(), session, &row); err != nil || session.status != "failed" {
					t.Fatalf("interrupted recovery did not finish: %v", err)
				}
			}
		})
	}
}
