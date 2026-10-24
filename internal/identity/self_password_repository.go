package identity

import "context"

// Compare-and-swap prevents a request verified against an old password from
// overwriting a concurrent password change. Audit and update commit together.
func (r *SQLRepository) ReplacePassword(ctx context.Context, user User, hash string, client Client) error {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE users SET password_hash=? WHERE id=? AND BINARY password_hash=? AND is_active=1", hash, user.ID, user.PasswordHash)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrCurrentPassword
	}
	if err = writeAudit(ctx, tx, "user.password_changed", &user, user.Username, client); err != nil {
		return err
	}
	return tx.Commit()
}
