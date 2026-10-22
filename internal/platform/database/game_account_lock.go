package database

import (
	"context"
	"database/sql/driver"
	"errors"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrAccountBusy = errors.New("账号操作繁忙，请稍后重试。")

// Game login uses MyISAM, so transactions/SELECT FOR UPDATE cannot serialize
// account changes. Both enqueue and mutation hold this connection-owned lock.
// Deletion intent remains the durable fence after the lock is released.
func LockAccountOperation(ctx context.Context, db *sqlx.DB, id int64) (*sqlx.Conn, func(), error) {
	conn, err := db.Connx(ctx)
	if err != nil {
		return nil, nil, err
	}
	release, err := LockAccountConnection(ctx, conn, id)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, func() { release(); conn.Close() }, nil
}

// LockAccountConnection also supports mail transactions on the same connection,
// avoiding a second pool checkout while holding a transaction and receipt lock.
func LockAccountConnection(ctx context.Context, conn *sqlx.Conn, id int64) (func(), error) {
	key := "happyro-account-operation:" + strconv.FormatInt(id, 10)
	var acquired int
	var err error
	if err = conn.GetContext(ctx, &acquired, "SELECT GET_LOCK(?,10)", key); err != nil || acquired != 1 {
		// A cancelled acquisition can still leave a server-side lock. Discard it.
		conn.Raw(func(any) error { return driver.ErrBadConn })
		if err == nil {
			err = ErrAccountBusy
		}
		return nil, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released int
		if err := conn.GetContext(cleanup, &released, "SELECT RELEASE_LOCK(?)", key); err != nil || released != 1 {
			conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}
	return release, nil
}
