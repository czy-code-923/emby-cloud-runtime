package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// EnsureTelegramProfile creates an identity, never playback access or admin rights.
// Telegram ID is authoritative; usernames cannot claim or merge an existing Web account.
func (s *Store) EnsureTelegramProfile(tgID int64, tgName, passwordHash string, limit int) (User, bool, error) {
	if tgID <= 0 {
		return User{}, false, ErrConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var result User
	created := false
	err := s.mutateAndSaveLocked(func() error {
		created = false
		if uid, ok := s.telegramIDMap[tgID]; ok {
			result = s.state.Users[uid]
			return errNoChange
		}
		if limit >= 0 && len(s.state.Users) >= limit {
			return fmt.Errorf("profile capacity reached")
		}
		name := fmt.Sprintf("tg_%d", tgID)
		if s.usernameTakenLocked(name, 0) {
			return ErrConflict
		}
		now := time.Now().Unix()
		result = User{UID: s.state.NextUserID, Username: name, TelegramID: tgID, TelegramUsername: tgName, PasswordHash: passwordHash, Role: RoleNormal, Active: true, ExpiredAt: -1, RegisterTime: now, CreatedAt: now}
		s.state.NextUserID++
		s.state.Users[result.UID] = result
		s.maintainUserIndexes(User{}, result, result.UID)
		created = true
		return nil
	})
	return result, created, err
}

func identityDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func ensureJianweiIdentitySchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS twilight_mini_sessions (
 token_hash text PRIMARY KEY, uid bigint NOT NULL, proof_hash text UNIQUE NOT NULL, nonce_hash text NOT NULL, bot_hash text NOT NULL,
 expires_at bigint NOT NULL, created_at bigint NOT NULL);
 ALTER TABLE twilight_mini_sessions ADD COLUMN IF NOT EXISTS telegram_id bigint NOT NULL DEFAULT 0;
 CREATE INDEX IF NOT EXISTS twilight_mini_sessions_expiry ON twilight_mini_sessions(expires_at)`)
	return err
}

func (s *Store) CreateMiniSession(ctx context.Context, uid, telegramID int64, proof, nonce, botKey, token string, now int64) error {
	if uid <= 0 || telegramID <= 0 {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO twilight_mini_sessions(token_hash,uid,proof_hash,nonce_hash,bot_hash,expires_at,created_at,telegram_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(proof_hash) DO NOTHING`, identityDigest(token), uid, identityDigest(proof), identityDigest(nonce), identityDigest(botKey), now+900, now, telegramID)
	if err != nil {
		return err
	}
	var storedUID, storedTelegramID, expiry int64
	var storedNonce, storedToken string
	err = s.db.QueryRowContext(ctx, `SELECT uid,telegram_id,nonce_hash,token_hash,expires_at FROM twilight_mini_sessions WHERE proof_hash=$1`, identityDigest(proof)).Scan(&storedUID, &storedTelegramID, &storedNonce, &storedToken, &expiry)
	if err != nil || storedUID != uid || storedTelegramID != telegramID || storedNonce != identityDigest(nonce) || storedToken != identityDigest(token) || expiry <= now {
		return ErrConflict
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM twilight_mini_sessions WHERE token_hash IN (SELECT token_hash FROM twilight_mini_sessions WHERE expires_at<$1 ORDER BY expires_at LIMIT 1000)`, now)
	return nil
}
func (s *Store) MiniSessionUser(ctx context.Context, token, botKey string, now int64) (User, error) {
	if len(token) != 64 {
		return User{}, ErrNotFound
	}
	var uid, telegramID int64
	if err := s.db.QueryRowContext(ctx, `SELECT uid,telegram_id FROM twilight_mini_sessions WHERE token_hash=$1 AND bot_hash=$2 AND expires_at>$3`, identityDigest(token), identityDigest(botKey), now).Scan(&uid, &telegramID); err != nil {
		return User{}, ErrNotFound
	}
	u, ok := s.User(uid)
	if !ok || !u.Active || telegramID <= 0 || u.TelegramID != telegramID {
		return User{}, ErrNotFound
	}
	return u, nil
}
