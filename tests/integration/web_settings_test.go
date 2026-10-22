package integration

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/settings"
)

// Lose one actual successful HTTP response after the real map has executed it.
// Recovery must use its execution receipt, not execute the mutation again.
type loseSettingsResponse struct {
	base http.RoundTripper
	lose bool
}

func (t *loseSettingsResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err == nil && t.lose && request.Method == "POST" && strings.HasSuffix(request.URL.Path, "/commands") {
		t.lose = false
		response.Body.Close()
		return nil, errors.New("injected lost game response")
	}
	return response, err
}
func exerciseRealWebSettingsRecovery(t *testing.T, directory, socket string, cfg *mysql.Config) {
	t.Helper()
	binaryPath := os.Getenv("TEST_WEB_SERVER_BINARY")
	if binaryPath == "" {
		t.Log("real web forwarding skipped: set TEST_WEB_SERVER_BINARY")
		return
	}
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	var inter strings.Builder
	for _, prefix := range []string{"login", "char", "map", "web"} {
		fmt.Fprintf(&inter, "%s_server_ip: %s\n%s_server_port: %s\n%s_server_id: %s\n%s_server_pw: %s\n%s_server_db: %s\n", prefix, host, prefix, port, prefix, cfg.User, prefix, cfg.Passwd, prefix, cfg.DBName)
	}
	if err = os.WriteFile(filepath.Join(directory, "conf/inter_athena.conf"), []byte(inter.String()), 0600); err != nil {
		t.Fatal(err)
	}
	webPort := availablePort(t)
	const token = "isolated-settings-control-token-1234567890"
	config := fmt.Sprintf("bind_ip: 127.0.0.1\nweb_port: %d\nprint_req_res: off\ngame_control_enabled: yes\ngame_control_secret: %s\ngame_control_socket: %s\n", webPort, token, socket)
	if err = os.WriteFile(filepath.Join(directory, "conf/web_athena.conf"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(directory, "web.log"))
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(binaryPath)
	process.Args[0] = "web-server"
	process.Dir = directory
	process.Stdout = log
	process.Stderr = log
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		process.Process.Kill()
		process.Wait()
		log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(log.Name())
			t.Log(string(raw))
		}
	})
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	dropped := &loseSettingsResponse{base: transport}
	gateway := &gameapi.HTTPGateway{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", webPort), Token: token, Client: &http.Client{Transport: dropped, Timeout: 5 * time.Second}}
	ctx := context.Background()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err = gateway.SettingsState(ctx); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("web not ready: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	unauthorized := *gateway
	unauthorized.Token = "wrong"
	if _, err = unauthorized.SettingsState(ctx); err == nil {
		t.Fatal("control API accepted invalid token")
	}
	before, err := gateway.SettingsState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	db := testDatabase(t)
	commands := &gameapi.Service{Commands: &gameapi.SQLRepository{DB: db}, Gateway: gateway, Timeout: 5 * time.Second}
	service := &settings.Service{Revisions: &settings.SQLRevisions{DB: db}, File: settings.File{Path: filepath.Join(directory, "published-battle.conf")}, Commands: commands, Gateway: gateway}
	actorID := int64(1)
	actor := gameapi.Actor{AdminUserID: &actorID}
	input := settings.Update{Key: "real-web-lost-response", Changes: map[string]int64{"refine_chance_rate": 175}}
	dropped.lose = true
	row, err := service.Apply(ctx, input, actor)
	var failure *gameapi.GatewayError
	if !errors.As(err, &failure) || !failure.OutcomeUnknown {
		t.Fatalf("expected uncertain outcome: %+v %v", row, err)
	}
	executed, err := gateway.SettingsState(ctx)
	if err != nil || executed.Revision != before.Revision+1 || executed.Values["refine_chance_rate"] != 175 {
		t.Fatalf("map did not apply: %+v %v", executed, err)
	}
	recovered, err := service.Resume(ctx, input, actor)
	if err != nil || recovered.Status != "applied" || recovered.Attempt != 0 {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	after, err := gateway.SettingsState(ctx)
	if err != nil || after.Revision != executed.Revision || after.LastCommandID != executed.LastCommandID {
		t.Fatalf("recovery repeated map command: %+v %v", after, err)
	}
	countRows(t, db, "game_server_commands", 1)
	var status string
	if err = db.Get(&status, "SELECT status FROM game_server_commands"); err != nil || status != "succeeded" {
		t.Fatalf("receipt not persisted: %s %v", status, err)
	}
	// Exercise the authenticated HTTP fence endpoint as well as direct socket CAS.
	if err = gateway.FenceSettings(ctx, after.InstanceID, after.Revision); err != nil {
		t.Fatal(err)
	}
	fenced, err := gateway.SettingsState(ctx)
	if err != nil || fenced.Revision != after.Revision+1 || fenced.LastStatus != "fenced" || fenced.Values["refine_chance_rate"] != 175 {
		t.Fatalf("HTTP barrier changed values: %+v %v", fenced, err)
	}
}
