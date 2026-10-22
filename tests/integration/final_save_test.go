package integration

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Compile the fixture against the same PACKETVER and native status layout as
// the server, rather than duplicating its C++ ABI in Go.
func finalSavePacket(t *testing.T, repo string) []byte {
	t.Helper()
	source := `#include <common/mmo.hpp>
#include <cstdio>
#include <cstring>
int main() {
 mmo_charstatus status{};
 status.account_id=2000000; status.char_id=150000; status.zeny=7654;
 unsigned char packet[13+sizeof(status)]{};
 uint16 opcode=0x2b01, length=sizeof(packet);
 memcpy(packet,&opcode,2); memcpy(packet+2,&length,2);
 memcpy(packet+4,&status.account_id,4); memcpy(packet+8,&status.char_id,4);
 packet[12]=1; memcpy(packet+13,&status,sizeof(status));
 return fwrite(packet,1,sizeof(packet),stdout)==sizeof(packet)?0:1;
}`
	dir := t.TempDir()
	src := filepath.Join(dir, "packet.cpp")
	out := filepath.Join(dir, "packet")
	if err := os.WriteFile(src, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("c++", "-std=c++17", "-DPACKETVER=20211103", "-I", filepath.Join(repo, "src"), src, "-o", out)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native save fixture: %v\n%s", err, output)
	}
	packet, err := exec.Command(out).Output()
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func readFinalSaveAck(conn net.Conn) ([]byte, error) {
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(conn, header); err != nil {
			return nil, err
		}
		switch binary.LittleEndian.Uint16(header) {
		case 0x2b21:
			ack := make([]byte, 8)
			_, err := io.ReadFull(conn, ack)
			return ack, err
		case 0x2b00:
			if _, err := io.CopyN(io.Discard, conn, 4); err != nil {
				return nil, err
			}
		case 0x388c:
			var length uint16
			if err := binary.Read(conn, binary.LittleEndian, &length); err != nil {
				return nil, err
			}
			if length < 4 {
				return nil, fmt.Errorf("invalid storage packet length %d", length)
			}
			if _, err := io.CopyN(io.Discard, conn, int64(length)-4); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unexpected char packet %x", header)
		}
	}
}

func TestRealCharFinalSaveAcknowledgesOnlyAfterSQLRecovery(t *testing.T) {
	binaryPath := os.Getenv("TEST_CHAR_SERVER_BINARY")
	if binaryPath == "" {
		t.Skip("set TEST_CHAR_SERVER_BINARY")
	}
	repo := filepath.Dir(binaryPath)
	packet := finalSavePacket(t, repo)
	db := testDatabase(t)
	cfg := fullGameSchema(t, db, repo)
	db.MustExec("INSERT INTO login (account_id,userid,user_pass,sex) VALUES (2000000,'save_fixture','password','M')")
	db.MustExec("INSERT INTO `char` (char_id,account_id,char_num,name,zeny,online) VALUES (150000,2000000,0,'SaveFixture',100,1)")
	// MyISAM counter records the failed attempt even when SIGNAL aborts UPDATE.
	db.MustExec("CREATE TABLE save_attempts (id INT) ENGINE=MyISAM")
	db.MustExec("CREATE TRIGGER reject_save BEFORE UPDATE ON `char` FOR EACH ROW BEGIN INSERT INTO save_attempts VALUES (1); SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated save failure'; END")
	connection := startDeletionWorker(t, binaryPath, cfg)
	type result struct {
		ack []byte
		err error
	}
	results := make(chan result, 1)
	connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	go func() { ack, err := readFinalSaveAck(connection); results <- result{ack, err} }()
	if _, err := connection.Write(packet); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(5 * time.Second)
	attempted := false
	for time.Now().Before(until) {
		var count int
		if err := db.Get(&count, "SELECT COUNT(*) FROM save_attempts"); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			attempted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !attempted {
		t.Fatal("server did not attempt the SQL save")
	}
	select {
	case response := <-results:
		t.Fatalf("failed save produced response: %x %v", response.ack, response.err)
	case <-time.After(200 * time.Millisecond):
	}
	var zeny, online int
	if err := db.QueryRow("SELECT zeny,online FROM `char` WHERE char_id=150000").Scan(&zeny, &online); err != nil {
		t.Fatal(err)
	}
	if zeny != 100 || online != 1 {
		t.Fatalf("failed save changed state: zeny=%d online=%d", zeny, online)
	}
	db.MustExec("DROP TRIGGER reject_save")
	if _, err := connection.Write(packet); err != nil {
		t.Fatal(err)
	}
	response := <-results
	if response.err != nil || len(response.ack) != 8 {
		t.Fatalf("save retry ack: %x %v", response.ack, response.err)
	}
	if binary.LittleEndian.Uint32(response.ack) != 2000000 || binary.LittleEndian.Uint32(response.ack[4:]) != 150000 {
		t.Fatalf("wrong save identity: %x", response.ack)
	}
	if err := db.QueryRow("SELECT zeny,online FROM `char` WHERE char_id=150000").Scan(&zeny, &online); err != nil {
		t.Fatal(err)
	}
	if zeny != 7654 || online != 0 {
		t.Fatalf("successful save state: zeny=%d online=%d", zeny, online)
	}
}
