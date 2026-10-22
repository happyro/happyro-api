package integration

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRealMapAndCharDeleteAutotradeSession(t *testing.T) {
	exerciseRealMapDeletion(t, false)
}
func TestRealMapAndCharRetryFailedFinalSave(t *testing.T) {
	exerciseRealMapDeletion(t, true)
}
func exerciseRealMapDeletion(t *testing.T, failSave bool) {
	charBinary, mapBinary := os.Getenv("TEST_CHAR_SERVER_BINARY"), os.Getenv("TEST_MAP_SERVER_BINARY")
	if charBinary == "" || mapBinary == "" {
		t.Skip("set TEST_CHAR_SERVER_BINARY and TEST_MAP_SERVER_BINARY")
	}
	db := testDatabase(t)
	cfg := fullGameSchema(t, db, filepath.Dir(charBinary))
	for _, statement := range []string{
		"INSERT INTO login(account_id,userid,user_pass,sex) VALUES(2000000,'live_delete','password','M')",
		"INSERT INTO `char`(char_id,account_id,name,sex,class,base_level,job_level,hp,max_hp,last_map,last_x,last_y,save_map,save_x,save_y) VALUES(150000,2000000,'DeleteMerchant','M',5,50,50,100,100,'prontera',150,150,'prontera',150,150)",
		"INSERT INTO skill(char_id,id,lv) VALUES(150000,1,9),(150000,36,10),(150000,39,10),(150000,41,10)",
		// SC_PUSH_CART=518 in src/map/status.hpp; restored merchants need its saved state.
		"INSERT INTO sc_data(account_id,char_id,type,tick,val1) VALUES(2000000,150000,518,-1,1)",
		"INSERT INTO cart_inventory(id,char_id,nameid,amount,identify) VALUES(1,150000,501,10,1)",
		"INSERT INTO vendings(id,account_id,char_id,map,x,y,title,autotrade) VALUES(1,2000000,150000,'prontera',150,150,'FixtureShop',1)",
		"INSERT INTO vending_items(vending_id,`index`,cartinventory_id,amount,price) VALUES(1,0,1,1,10)",
	} {
		db.MustExec(statement)
	}
	port := startDeletionCharServer(t, charBinary, cfg)
	_, socket, _, _ := startRealMap(t, mapBinary, cfg, port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, _, err := callRealMap(socket, map[string]any{"type": "character.snapshot", "id": "live-session", "target": map[string]any{"type": "character", "id": 150000}, "payload": map[string]any{}})
		if err == nil && status == 200 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("autotrade session unavailable: status=%d err=%v", status, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Wait until the recovered shop is fully opened before requesting deletion.
	deadline = time.Now().Add(15 * time.Second)
	for {
		var count int
		if err := db.Get(&count, "SELECT COUNT(*) FROM vendings WHERE char_id=150000"); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("autotrade shop did not reopen")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if failSave {
		db.MustExec("CREATE TABLE save_attempts (id INT) ENGINE=MyISAM")
		db.MustExec("CREATE TRIGGER reject_save BEFORE UPDATE ON `char` FOR EACH ROW BEGIN INSERT INTO save_attempts VALUES (1); SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated map save failure'; END")
		// Change live state so final save must write it even if the shop's initial
		// save had no other changes. The normal save also exercises failure handling.
		status, body, err := callRealMap(socket, map[string]any{"type": "character.currency.zeny_grant", "id": "dirty-before-logout", "target": map[string]any{"type": "character", "id": 150000}, "payload": map[string]any{"amount": 123}})
		if err != nil || status != 200 {
			t.Fatalf("live mutation: %d %v %+v", status, err, body)
		}
	}
	db.MustExec("INSERT INTO happyro_admin_account_deletions(account_id,requested_by,requested_username,account_name) VALUES(2000000,1,'admin','live_delete')")
	if failSave {
		deadline = time.Now().Add(10 * time.Second)
		for {
			var attempts int
			var status string
			if err := db.Get(&attempts, "SELECT COUNT(*) FROM save_attempts"); err != nil {
				t.Fatal(err)
			}
			if err := db.Get(&status, "SELECT status FROM happyro_admin_account_deletions WHERE account_id=2000000"); err != nil {
				t.Fatal(err)
			}
			if status == "completed" || status == "failed" {
				t.Fatalf("deletion passed failed save: %s", status)
			}
			if attempts >= 2 && status == "disconnecting" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("no failed logout save: attempts=%d status=%s", attempts, status)
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Let multiple worker ticks pass; the real map must retain its logout node.
		until := time.Now().Add(2200 * time.Millisecond)
		for time.Now().Before(until) {
			var status string
			if err := db.Get(&status, "SELECT status FROM happyro_admin_account_deletions WHERE account_id=2000000"); err != nil {
				t.Fatal(err)
			}
			if status != "disconnecting" {
				t.Fatalf("failed save released deletion: %s", status)
			}
			countRows(t, db, "`char`", 1)
			time.Sleep(100 * time.Millisecond)
		}
		db.MustExec("DROP TRIGGER reject_save")
		// No manual save or reconnect: wait for the production map logout retry
		// (60-second threshold checked by a 30-second timer) to release the fence.
		deadline = time.Now().Add(110 * time.Second)
		for {
			var status string
			if err := db.Get(&status, "SELECT status FROM happyro_admin_account_deletions WHERE account_id=2000000"); err != nil {
				t.Fatal(err)
			}
			if status == "completed" {
				break
			}
			if status == "failed" || time.Now().After(deadline) {
				t.Fatalf("automatic save recovery failed: %s", status)
			}
			time.Sleep(100 * time.Millisecond)
		}
	} else {
		awaitDeletionStatus(t, db, "completed")
	}

	var remaining int
	if err := db.Get(&remaining, "SELECT COUNT(*) FROM login WHERE account_id=2000000"); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatal("deleted account remains")
	}
	for _, table := range []string{"`char`", "cart_inventory", "vendings", "vending_items"} {
		countRows(t, db, table, 0)
	}
}
