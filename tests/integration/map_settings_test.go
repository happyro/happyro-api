package integration

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealMapSettingsAdmissionAndBarrier(t *testing.T) {
	binaryPath := os.Getenv("TEST_MAP_SERVER_BINARY")
	if binaryPath == "" {
		t.Skip("set TEST_MAP_SERVER_BINARY to an isolated-test map-server")
	}
	repo := filepath.Dir(binaryPath)
	db := testDatabase(t)
	cfg := fullGameSchema(t, db, repo)
	charListener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { charListener.Close() })
	directory, socket, call, body := startRealMap(t, binaryPath, cfg, charListener.Addr().(*net.TCPAddr).Port)
	state := body["data"].(map[string]any)["result"].(map[string]any)
	instance := state["instance_id"].(string)
	payload := map[string]any{"instance_id": instance, "expected_revision": 0, "changes": []any{map[string]any{"key": "refine_chance_rate", "value": 125}}}
	for _, test := range []struct {
		kind, id string
		payload  map[string]any
		status   int
	}{
		{"battle_config.apply", "original", payload, 200},
		{"battle_config.apply", "original", payload, 409},
		{"battle_config.fence", "barrier", map[string]any{"instance_id": instance, "expected_revision": 1}, 200},
	} {
		status, _, err := call(test.kind, test.id, test.payload)
		if err != nil || status != test.status {
			t.Fatalf("%s: %d %v", test.id, status, err)
		}
	}
	payload["expected_revision"] = 1
	if status, _, err := call("battle_config.apply", "late", payload); err != nil || status != 409 {
		t.Fatalf("late request accepted: %d %v", status, err)
	}
	status, body, err := call("battle_config.read", "final", map[string]any{})
	if err != nil || status != 200 {
		t.Fatal(err)
	}
	state = body["data"].(map[string]any)["result"].(map[string]any)
	if state["revision"] != float64(2) || state["last_status"] != "fenced" || state["values"].(map[string]any)["refine_chance_rate"] != float64(125) {
		t.Fatalf("incorrect map state: %+v", state)
	}
	// Exercise real map-side deletion acknowledgements over its char connection.
	charListener.SetDeadline(time.Now().Add(15 * time.Second))
	charConnection, err := charListener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer charConnection.Close()
	charConnection.SetDeadline(time.Now().Add(10 * time.Second))
	login := make([]byte, 60)
	if _, err = io.ReadFull(charConnection, login); err != nil || binary.LittleEndian.Uint16(login) != 0x2af8 {
		t.Fatalf("map login: %x %v", login, err)
	}
	if _, err = charConnection.Write([]byte{0xf9, 0x2a, 0}); err != nil {
		t.Fatal(err)
	}
	// Initialization may emit map lists and other inter-server queries before the
	// fence response. Match its complete 14-byte envelope, including unique nonce.
	for _, nonce := range []uint64{0x8877665544332211, 0x1122334455667788} {
		fence := make([]byte, 14)
		binary.LittleEndian.PutUint16(fence, 0x2b29)
		binary.LittleEndian.PutUint32(fence[2:], 2000000)
		binary.LittleEndian.PutUint64(fence[6:], nonce)
		if _, err = charConnection.Write(fence); err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint16(fence, 0x2b2c)
		received := []byte{}
		for !bytes.Contains(received, fence) {
			chunk := make([]byte, 4096)
			n, readErr := charConnection.Read(chunk)
			if readErr != nil {
				t.Fatalf("missing deletion ack: %v", readErr)
			}
			received = append(received, chunk[:n]...)
			if len(received) > 1<<20 {
				t.Fatal("excessive inter-server data")
			}
		}
	}
	exerciseRealWebSettingsRecovery(t, directory, socket, cfg)
}

// startRealMap connects an isolated map process to either a real or fixture char peer.
func startRealMap(t *testing.T, binaryPath string, cfg *mysql.Config, charPort int) (string, string, func(string, string, map[string]any) (int, map[string]any, error), map[string]any) {
	t.Helper()
	repo := filepath.Dir(binaryPath)
	directory := t.TempDir()
	// Copy defaults but never inherit local credentials or imports.
	err := filepath.WalkDir(filepath.Join(repo, "conf"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(filepath.Join(repo, "conf"), path)
		if entry.IsDir() {
			if relative == "import" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(directory, "conf", relative), 0700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := []string{}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "import:") && strings.Contains(line, "conf/import/") {
				continue
			}
			lines = append(lines, line)
		}
		return os.WriteFile(filepath.Join(directory, "conf", relative), []byte(strings.Join(lines, "\n")), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"db", "npc"} {
		if err = os.Symlink(filepath.Join(repo, name), filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	os.Mkdir(filepath.Join(directory, "log"), 0700)
	// A short dedicated socket directory avoids Unix sun_path limits on macOS.
	socketDir, err := os.MkdirTemp("/tmp", "hro-map-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })
	socket := filepath.Join(socketDir, "control.sock")
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	inter := fmt.Sprintf("map_server_ip: %s\nmap_server_port: %s\nmap_server_id: %s\nmap_server_pw: %s\nmap_server_db: %s\nlog_db_ip: %s\nlog_db_port: %s\nlog_db_id: %s\nlog_db_pw: %s\nlog_db_db: %s\n", host, port, cfg.User, cfg.Passwd, cfg.DBName, host, port, cfg.User, cfg.Passwd, cfg.DBName)
	if err = os.WriteFile(filepath.Join(directory, "inter.conf"), []byte(inter), 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("userid: go_delete_test\npasswd: go_delete_test\nbind_ip: 127.0.0.1\nmap_ip: 127.0.0.1\nchar_ip: 127.0.0.1\nchar_port: %d\nmap_port: %d\nmap: prontera\ndb_path: db\nuse_grf: no\ngame_control_socket: %s\n", charPort, availablePort(t), socket)
	os.WriteFile(filepath.Join(directory, "map.conf"), []byte(config), 0600)
	log, err := os.Create(filepath.Join(directory, "map.log"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binaryPath, "--map-config", "map.conf", "--inter-config", "inter.conf")
	command.Args[0] = "map-server" // Prevent the server core from changing to the binary directory.
	command.Dir = directory
	command.Stdout = log
	command.Stderr = log
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		command.Process.Kill()
		command.Wait()
		log.Close()
		if t.Failed() {
			raw, _ := os.ReadFile(log.Name())
			t.Logf("map log: %s", raw)
		}
	})
	call := func(kind, id string, payload map[string]any) (int, map[string]any, error) {
		return callRealMap(socket, map[string]any{"type": kind, "id": id, "payload": payload})
	}
	var body map[string]any
	deadline := time.Now().Add(45 * time.Second)
	for {
		status, result, callErr := call("battle_config.read", "read", map[string]any{})
		if callErr == nil && status == 200 {
			body = result
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(log.Name())
			t.Fatalf("map unavailable: %v\n%s", callErr, raw)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return directory, socket, call, body
}

func callRealMap(socket string, request map[string]any) (int, map[string]any, error) {
	connection, err := net.DialTimeout("unix", socket, time.Second)
	if err != nil {
		return 0, nil, err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(10 * time.Second))
	raw, _ := json.Marshal(request)
	if _, err = connection.Write(raw); err != nil {
		return 0, nil, err
	}
	connection.(*net.UnixConn).CloseWrite()
	reader := bufio.NewReader(connection)
	var status int
	if _, err = fmt.Fscanln(reader, &status); err != nil {
		return 0, nil, err
	}
	var body map[string]any
	err = json.NewDecoder(reader).Decode(&body)
	return status, body, err
}
