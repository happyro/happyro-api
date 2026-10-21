package integration

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

func fullGameSchema(t *testing.T, db *sqlx.DB, repo string) *mysql.Config {
	t.Helper()
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_ADMIN_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Get(&cfg.DBName, "SELECT DATABASE()"); err != nil {
		t.Fatal(err)
	}
	cfg.MultiStatements = true
	connection, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	for _, name := range []string{"main.sql", "web.sql", "logs.sql", "upgrades/upgrade_20261012_sessions.sql"} {
		raw, err := os.ReadFile(filepath.Join(repo, "sql-files", name))
		if err != nil {
			t.Fatal(err)
		}
		// Load the session table without the client DELIMITER/trigger statements;
		// this test exercises cleanup, not session revocation triggers.
		source := strings.Split(string(raw), "-- Revoke immediately")[0]
		if _, err = connection.Exec(source); err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
	}
	if err = prepareDeletions(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return cfg
}
func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}
func startDeletionCharServer(t *testing.T, binaryPath string, cfg *mysql.Config) int {
	t.Helper()
	repo := filepath.Dir(binaryPath)
	directory := t.TempDir()
	for _, name := range []string{"db", "conf"} {
		if err := os.Symlink(filepath.Join(repo, name), filepath.Join(directory, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "log"), 0700); err != nil {
		t.Fatal(err)
	}
	host, dbPort, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	loginListener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { loginListener.Close() })
	charPort := availablePort(t)
	character := fmt.Sprintf("userid: go_delete_test\npasswd: go_delete_test\nbind_ip: 127.0.0.1\nchar_ip: 127.0.0.1\nchar_port: %d\nlogin_ip: 127.0.0.1\nlogin_port: %d\nchar_check_db: yes\nlog_char: no\n", charPort, loginListener.Addr().(*net.TCPAddr).Port)
	inter := fmt.Sprintf("char_server_ip: %s\nchar_server_port: %s\nchar_server_id: %s\nchar_server_pw: %s\nchar_server_db: %s\nlog_db_ip: %s\nlog_db_port: %s\nlog_db_id: %s\nlog_db_pw: %s\nlog_db_db: %s\nlog_inter: no\n", host, dbPort, cfg.User, cfg.Passwd, cfg.DBName, host, dbPort, cfg.User, cfg.Passwd, cfg.DBName)
	// Load the repository's table mappings and gameplay defaults, excluding local
	// imports so this process can only use the disposable database credentials.
	for name, destination := range map[string]*string{"char_athena.conf": &character, "inter_athena.conf": &inter} {
		raw, readErr := os.ReadFile(filepath.Join(repo, "conf", name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		var defaults strings.Builder
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "import:") {
				defaults.WriteString(line + "\n")
			}
		}
		*destination = defaults.String() + *destination
	}
	for name, content := range map[string]string{"char.conf": character, "inter.conf": inter, "lan.conf": ""} {
		if err = os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	log, err := os.Create(filepath.Join(directory, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(binaryPath, "--char-config", filepath.Join(directory, "char.conf"), "--inter-config", filepath.Join(directory, "inter.conf"), "--lan-config", filepath.Join(directory, "lan.conf"))
	process.Args[0] = "char-server" // Keep Core::start inside the isolated working directory.
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
			raw, _ := os.ReadFile(filepath.Join(directory, "server.log"))
			t.Logf("char-server log: %s", raw)
		}
	})
	loginListener.SetDeadline(time.Now().Add(15 * time.Second))
	login, err := loginListener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { login.Close() })
	login.SetReadDeadline(time.Now().Add(5 * time.Second))
	loginRequest := make([]byte, 86)
	if _, err = io.ReadFull(login, loginRequest); err != nil || binary.LittleEndian.Uint16(loginRequest) != 0x2710 {
		t.Fatalf("login handshake: %v", err)
	}
	if _, err = login.Write([]byte{0x11, 0x27, 0}); err != nil {
		t.Fatal(err)
	}
	login.SetReadDeadline(time.Time{})
	go serveFixtureLogin(t, login)
	return charPort
}

func startDeletionWorker(t *testing.T, binaryPath string, cfg *mysql.Config) net.Conn {
	t.Helper()
	charPort := startDeletionCharServer(t, binaryPath, cfg)
	var err error
	var connection net.Conn
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		connection, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", charPort), time.Second)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("isolated char-server did not listen")
	}
	t.Cleanup(func() { connection.Close() })
	request := make([]byte, 60)
	binary.LittleEndian.PutUint16(request, 0x2af8)
	copy(request[2:26], "go_delete_test")
	copy(request[26:50], "go_delete_test")
	copy(request[54:58], net.ParseIP("127.0.0.1").To4())
	binary.BigEndian.PutUint16(request[58:], 5121)
	if _, err = connection.Write(request); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	ack := make([]byte, 3)
	if _, err = io.ReadFull(connection, ack); err != nil || binary.LittleEndian.Uint16(ack) != 0x2af9 || ack[2] != 0 {
		t.Fatalf("map handshake %x %v", ack, err)
	}
	return connection
}
func nextFence(t *testing.T, connection net.Conn) []byte {
	t.Helper()
	connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(connection, header); err != nil {
			t.Fatal(err)
		}
		switch binary.LittleEndian.Uint16(header) {
		case 0x2b29:
			packet := make([]byte, 14)
			copy(packet, header)
			if _, err := io.ReadFull(connection, packet[2:]); err != nil {
				t.Fatal(err)
			}
			return packet
		case 0x2b00:
			if _, err := io.CopyN(io.Discard, connection, 4); err != nil {
				t.Fatal(err)
			}
		case 0x388c: // Storage configuration sent during map initialization.
			var length uint16
			if err := binary.Read(connection, binary.LittleEndian, &length); err != nil {
				t.Fatal(err)
			}
			if length < 4 {
				t.Fatalf("invalid storage packet length %d", length)
			}
			if _, err := io.CopyN(io.Discard, connection, int64(length)-4); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected map packet %x", header)
		}
	}
}
func awaitDeletionStatus(t *testing.T, db *sqlx.DB, want string) {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		var status string
		if err := db.Get(&status, "SELECT status FROM happyro_admin_account_deletions WHERE account_id=2000000"); err != nil {
			t.Fatal(err)
		}
		if status == want {
			return
		}
		if status == "failed" {
			var code string
			db.Get(&code, "SELECT error_code FROM happyro_admin_account_deletions WHERE account_id=2000000")
			t.Fatalf("worker failed: %s", code)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("deletion did not reach %s", want)
}
func TestRealCharWorkerWaitsForSaveFenceAndCleansAccount(t *testing.T) {
	for _, failCleanup := range []bool{false, true} {
		t.Run(fmt.Sprintf("cleanup_failure_%t", failCleanup), func(t *testing.T) { exerciseDeletionWorker(t, failCleanup) })
	}
}
func exerciseDeletionWorker(t *testing.T, failCleanup bool) {
	binaryPath := os.Getenv("TEST_CHAR_SERVER_BINARY")
	if binaryPath == "" {
		t.Skip("set TEST_CHAR_SERVER_BINARY to a built isolated-test char-server")
	}
	db := testDatabase(t)
	cfg := fullGameSchema(t, db, filepath.Dir(binaryPath))
	for _, statement := range []string{
		"INSERT INTO login(account_id,userid,user_pass,email,sex) VALUES(2000000,'delete_me','secret','a@a.com','M'),(2000001,'keep_me','secret','b@b.com','F')",
		"INSERT INTO `char`(char_id,account_id,name,sex)VALUES(150000,2000000,'Delete Character','M'),(150001,2000001,'Keep Character','F')",
		"INSERT INTO inventory(char_id,nameid,amount)VALUES(150000,501,1),(150001,501,1)",
		"INSERT INTO storage(account_id,nameid,amount)VALUES(2000000,501,1),(2000001,501,1)",
		"INSERT INTO skill(char_id,id,lv)VALUES(150000,1,1),(150001,410,1)",
		"UPDATE `char` SET father=150001 WHERE char_id=150000",
		"UPDATE `char` SET child=150000 WHERE char_id=150001",
		"INSERT INTO friends(char_id,friend_id)VALUES(150001,150000)",
		"INSERT INTO vendings(id,account_id,char_id,map,x,y,title,autotrade)VALUES(1,2000000,150000,'prontera',1,1,'shop',1)",
		"INSERT INTO vending_items(vending_id,`index`,cartinventory_id,amount,price)VALUES(1,0,1,1,1)",
		"INSERT INTO buyingstores(id,account_id,char_id,map,x,y,title,`limit`,autotrade)VALUES(1,2000000,150000,'prontera',1,1,'shop',100,1)",
		"INSERT INTO buyingstore_items(buyingstore_id,`index`,item_id,amount,price)VALUES(1,0,501,1,1)",
		"INSERT INTO pet(pet_id,account_id,char_id,incubate) VALUES(65537,2000001,150001,1),(65538,2000000,150000,1)",
		"INSERT INTO mail(id,send_id,dest_id,`time`) VALUES(1,150001,150000,UNIX_TIMESTAMP()),(2,150000,150001,UNIX_TIMESTAMP())",
		"INSERT INTO mail_attachments(id,`index`,nameid,amount,card0,card1,card2) VALUES(1,0,9001,1,256,1,1),(2,0,9001,1,256,2,1)",
		"INSERT INTO market(name,nameid,price,amount)VALUES('npc-shop',501,50,10)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	connection := startDeletionWorker(t, binaryPath, cfg)
	if _, err := db.Exec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name)VALUES(2000000,1,'admin','delete_me')"); err != nil {
		t.Fatal(err)
	}
	fence := nextFence(t, connection)
	countRows(t, db, "`char`", 2)
	countRows(t, db, "inventory", 2)
	// An unrelated or stale acknowledgement must not release the deletion fence.
	stale := append([]byte{}, fence...)
	binary.LittleEndian.PutUint16(stale, 0x2b2c)
	stale[6] ^= 1
	connection.Write(stale)
	fence = nextFence(t, connection)
	countRows(t, db, "`char`", 2)
	if failCleanup {
		if _, err := db.Exec("RENAME TABLE skill TO skill_unavailable"); err != nil {
			t.Fatal(err)
		}
	}
	binary.LittleEndian.PutUint16(fence, 0x2b2c)
	if _, err := connection.Write(fence); err != nil {
		t.Fatal(err)
	}
	if failCleanup {
		awaitDeletionStatus(t, db, "failed")
		countRows(t, db, "`char`", 2)
		if _, err := db.Exec("RENAME TABLE skill_unavailable TO skill"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("UPDATE happyro_admin_account_deletions SET status='queued',error_code=NULL WHERE account_id=2000000"); err != nil {
			t.Fatal(err)
		}
		retryFence := nextFence(t, connection)
		if binary.LittleEndian.Uint64(retryFence[6:]) == binary.LittleEndian.Uint64(fence[6:]) {
			t.Fatal("retry reused old fence")
		}
		binary.LittleEndian.PutUint16(retryFence, 0x2b2c)
		if _, err := connection.Write(retryFence); err != nil {
			t.Fatal(err)
		}
	}
	awaitDeletionStatus(t, db, "completed")
	for _, table := range []string{"vendings", "vending_items", "buyingstores", "buyingstore_items"} {
		countRows(t, db, table, 0)
	}
	countRows(t, db, "pet", 1)
	countRows(t, db, "mail", 1)
	countRows(t, db, "mail_attachments", 1)
	var retainedPet, retainedMail int
	if err := db.Get(&retainedPet, "SELECT COUNT(*) FROM pet WHERE pet_id=65538 AND account_id=0"); err != nil || retainedPet != 1 {
		t.Fatalf("transferred pet removed: %d %v", retainedPet, err)
	}
	if err := db.Get(&retainedMail, "SELECT COUNT(*) FROM mail WHERE id=2 AND dest_id=150001 AND send_id=0"); err != nil || retainedMail != 1 {
		t.Fatalf("other recipient mail changed: %d %v", retainedMail, err)
	}
	countRows(t, db, "market", 1)
	countRows(t, db, "`char`", 1)
	countRows(t, db, "inventory", 1)
	countRows(t, db, "storage", 1)
	countRows(t, db, "skill", 0)
	countRows(t, db, "friends", 0)
	var survivingChild int
	if err := db.Get(&survivingChild, "SELECT child FROM `char` WHERE char_id=150001"); err != nil || survivingChild != 0 {
		t.Fatalf("parent still references deleted child: %d %v", survivingChild, err)
	}
	var remaining int
	if err := db.Get(&remaining, "SELECT COUNT(*) FROM login WHERE account_id=2000000"); err != nil || remaining != 0 {
		t.Fatalf("account left behind %d %v", remaining, err)
	}
}

func TestRealCharWorkerPreservesActiveAuctionBeforeCleanup(t *testing.T) {
	binaryPath := os.Getenv("TEST_CHAR_SERVER_BINARY")
	if binaryPath == "" {
		t.Skip("set TEST_CHAR_SERVER_BINARY")
	}
	for _, seller := range []bool{true, false} {
		t.Run(fmt.Sprintf("seller_%t", seller), func(t *testing.T) {
			db := testDatabase(t)
			cfg := fullGameSchema(t, db, filepath.Dir(binaryPath))
			for _, statement := range []string{
				"INSERT INTO login(account_id,userid,user_pass,sex) VALUES(2000000,'delete_me','secret','M'),(2000001,'keep_me','secret','F')",
				"INSERT INTO `char`(char_id,account_id,name,sex) VALUES(150000,2000000,'Delete Character','M'),(150001,2000001,'Keep Character','F')",
				"INSERT INTO inventory(char_id,nameid,amount) VALUES(150000,501,1)",
			} {
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			sellerID, buyerID := 150000, 150001
			if !seller {
				sellerID, buyerID = buyerID, sellerID
			}
			if _, err := db.Exec("INSERT INTO auction(auction_id,seller_id,buyer_id,price,buynow,`timestamp`,nameid,item_name)VALUES(1,?,?,100,200,UNIX_TIMESTAMP()+3600,501,'Red Potion')", sellerID, buyerID); err != nil {
				t.Fatal(err)
			}
			connection := startDeletionWorker(t, binaryPath, cfg)
			if _, err := db.Exec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','delete_me')"); err != nil {
				t.Fatal(err)
			}
			fence := nextFence(t, connection)
			binary.LittleEndian.PutUint16(fence, 0x2b2c)
			if _, err := connection.Write(fence); err != nil {
				t.Fatal(err)
			}
			awaitDeletionStatus(t, db, "failed")
			var code string
			if err := db.Get(&code, "SELECT error_code FROM happyro_admin_account_deletions"); err != nil || code != "active_auction" {
				t.Fatalf("wrong reason: %s %v", code, err)
			}
			countRows(t, db, "`char`", 2)
			countRows(t, db, "inventory", 1)
			countRows(t, db, "auction", 1)
			var escrow int
			if err := db.Get(&escrow, "SELECT COUNT(*) FROM auction WHERE seller_id=? AND buyer_id=? AND price=100 AND nameid=501", sellerID, buyerID); err != nil || escrow != 1 {
				t.Fatalf("escrow changed: %d %v", escrow, err)
			}
		})
	}
}

func TestRealCharWorkerClearsDeletedParentAndKeepsSurvivingFamily(t *testing.T) {
	binaryPath := os.Getenv("TEST_CHAR_SERVER_BINARY")
	if binaryPath == "" {
		t.Skip("set TEST_CHAR_SERVER_BINARY")
	}
	db := testDatabase(t)
	cfg := fullGameSchema(t, db, filepath.Dir(binaryPath))
	for _, statement := range []string{
		"INSERT INTO login(account_id,userid,user_pass,sex) VALUES(2000000,'delete_me','secret','M'),(2000001,'keep_me','secret','F')",
		"INSERT INTO `char`(char_id,account_id,name,sex,child) VALUES(150000,2000000,'Deleted Father','M',150002),(150001,2000001,'Surviving Mother','F',150002),(150002,2000001,'Surviving Child','M',0)",
		"UPDATE `char` SET father=150000,mother=150001 WHERE char_id=150002",
		"INSERT INTO skill(char_id,id,lv) VALUES(150001,410,1),(150002,408,1),(150002,409,1)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	connection := startDeletionWorker(t, binaryPath, cfg)
	if _, err := db.Exec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','delete_me')"); err != nil {
		t.Fatal(err)
	}
	fence := nextFence(t, connection)
	binary.LittleEndian.PutUint16(fence, 0x2b2c)
	if _, err := connection.Write(fence); err != nil {
		t.Fatal(err)
	}
	awaitDeletionStatus(t, db, "completed")
	var preserved int
	if err := db.Get(&preserved, "SELECT COUNT(*) FROM `char` WHERE char_id=150002 AND father=0 AND mother=150001"); err != nil || preserved != 1 {
		t.Fatalf("child family incorrect: %d %v", preserved, err)
	}
	if err := db.Get(&preserved, "SELECT COUNT(*) FROM `char` WHERE char_id=150001 AND child=150002"); err != nil || preserved != 1 {
		t.Fatalf("surviving parent changed: %d %v", preserved, err)
	}
	countRows(t, db, "skill", 3)
}

// serveFixtureLogin provides empty global account registries for restored
// autotrade sessions; ordinary bookkeeping packets need no reply.
func serveFixtureLogin(t *testing.T, conn net.Conn) {
	for {
		var opcode uint16
		if binary.Read(conn, binary.LittleEndian, &opcode) != nil {
			return
		}
		length := 0
		switch opcode {
		case 0x2737:
			continue
		case 0x2719:
			if _, err := conn.Write([]byte{0x18, 0x27}); err != nil {
				return
			}
			continue
		case 0x2714, 0x2716, 0x272b, 0x272c:
			length = 4
		case 0x272d, 0x2728:
			var total uint16
			if binary.Read(conn, binary.LittleEndian, &total) != nil || total < 4 {
				return
			}
			length = int(total) - 4
		case 0x272e:
			ids := make([]byte, 8)
			if _, err := io.ReadFull(conn, ids); err != nil {
				return
			}
			response := make([]byte, 16)
			binary.LittleEndian.PutUint16(response, 0x2726)
			binary.LittleEndian.PutUint16(response[2:], 16)
			copy(response[4:], ids)
			response[12] = 1 // account-wide registry completion
			if _, err := conn.Write(response); err != nil {
				return
			}
			continue
		default:
			t.Errorf("unexpected fixture login packet: %#x", opcode)
			return
		}
		if _, err := io.CopyN(io.Discard, conn, int64(length)); err != nil {
			return
		}
	}
}
