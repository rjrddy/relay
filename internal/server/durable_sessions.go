package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// EnableDurableSessions shares hashed, revocable cookie sessions across API instances.
// It creates only its own auth table in the engine's explicitly selected schema.
func (s *Server) EnableDurableSessions(ctx context.Context) error {
	_, err := s.Engine.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS relay_operator_sessions (
 token_hash bytea PRIMARY KEY, csrf text NOT NULL, expires timestamptz NOT NULL)`)
	if err == nil {
		s.DurableSessions = true
	}
	return err
}
func (s *Server) getSession(ctx context.Context, token string) (session, bool, error) {
	if !s.DurableSessions {
		s.mu.Lock()
		defer s.mu.Unlock()
		v, ok := s.sessions[token]
		if ok && time.Now().After(v.Expires) {
			delete(s.sessions, token)
			ok = false
		}
		return v, ok, nil
	}
	hash := sha256.Sum256([]byte(token))
	var v session
	err := s.Engine.Pool.QueryRow(ctx, "SELECT expires,csrf FROM relay_operator_sessions WHERE token_hash=$1 AND expires>clock_timestamp()", hash[:]).Scan(&v.Expires, &v.CSRF)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, false, nil
	}
	return v, err == nil, err
}
func (s *Server) storeSession(ctx context.Context, token string, v session) error {
	if !s.DurableSessions {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, x := range s.sessions {
			if time.Now().After(x.Expires) {
				delete(s.sessions, k)
			}
		}
		if len(s.sessions) >= 100 {
			return errSessionCapacity
		}
		s.sessions[token] = v
		return nil
	}
	tx, err := s.Engine.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "LOCK TABLE relay_operator_sessions IN EXCLUSIVE MODE"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM relay_operator_sessions WHERE expires<=clock_timestamp()"); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM relay_operator_sessions").Scan(&count); err != nil {
		return err
	}
	if count >= 100 {
		return errSessionCapacity
	}
	hash := sha256.Sum256([]byte(token))
	if _, err = tx.Exec(ctx, "INSERT INTO relay_operator_sessions(token_hash,csrf,expires) VALUES($1,$2,$3)", hash[:], v.CSRF, v.Expires); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Server) deleteSession(ctx context.Context, token string) error {
	if !s.DurableSessions {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return nil
	}
	hash := sha256.Sum256([]byte(token))
	_, err := s.Engine.Pool.Exec(ctx, "DELETE FROM relay_operator_sessions WHERE token_hash=$1", hash[:])
	return err
}

var errSessionCapacity = errors.New("session capacity")
