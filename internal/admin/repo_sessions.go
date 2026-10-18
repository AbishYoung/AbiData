package admin

import (
	"context"
	"time"

	"abidata/internal/dbx"
)

func (s *Store) CreateSession(ctx context.Context, se Session) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlCreateSession.SQL,
			se.IDHash, se.UserID, se.CSRFToken, se.CreatedAt, se.LastSeenAt, se.ExpiresAt, se.IP, se.UserAgent)
		return wrapWrite(err, "create session")
	})
}

func (s *Store) GetSession(ctx context.Context, idHash string) (Session, error) {
	var se Session
	err := s.Read(ctx, func(c dbx.Querier) error {
		var created, seen, expires string
		if err := c.QueryRow(ctx, sqlGetSession.SQL, idHash).
			Scan(&se.IDHash, &se.UserID, &se.CSRFToken, &created, &seen, &expires, &se.IP, &se.UserAgent); err != nil {
			return err
		}
		var err error
		if se.CreatedAt, err = parseTime(created); err != nil {
			return err
		}
		if se.LastSeenAt, err = parseTime(seen); err != nil {
			return err
		}
		se.ExpiresAt, err = parseTime(expires)
		return err
	})
	if err != nil {
		return Session{}, notFoundIfNoRows(err, "session")
	}
	return se, nil
}

func (s *Store) TouchSession(ctx context.Context, idHash string, lastSeen time.Time) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlTouchSession.SQL, lastSeen, idHash)
		if err != nil {
			return err
		}
		return requireChanged(res, "touch session")
	})
}

func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlDeleteSession.SQL, idHash)
		if err != nil {
			return err
		}
		return requireChanged(res, "delete session")
	})
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlDeleteUserSessions.SQL, userID)
		return err
	})
}

func (s *Store) PurgeSessions(ctx context.Context, now time.Time, idle time.Duration) (int64, error) {
	var n int64
	err := s.Write(ctx, func(tx dbx.Tx) error {
		res, err := tx.Exec(ctx, sqlPurgeSessions.SQL,
			now, now.Add(-idle))
		if err != nil {
			return err
		}
		n = res.Changes
		// Throttle counters (sign-in and failed API-key attempts, one row per
		// address) are dead after their window, which is at most 15 minutes.
		_, err = tx.Exec(ctx, sqlPurgeSessions2.SQL, now.Add(-time.Hour))
		return err
	})
	return n, err
}

func (s *Store) GetLoginAttempts(ctx context.Context, key string) (LoginAttempts, error) {
	var la LoginAttempts
	err := s.Read(ctx, func(c dbx.Querier) error {
		var err error
		la, err = readAttempts(ctx, c, key)
		return err
	})
	if dbx.IsNoRows(err) {
		return LoginAttempts{}, nil
	}
	return la, err
}

func readAttempts(ctx context.Context, q dbx.Querier, key string) (LoginAttempts, error) {
	la := LoginAttempts{Key: key}
	var ws string
	if err := q.QueryRow(ctx, sqlReadAttempts.SQL, key).
		Scan(&la.Count, &ws); err != nil {
		return LoginAttempts{}, err
	}
	var err error
	la.WindowStart, err = parseTime(ws)
	return la, err
}

func (s *Store) RecordLoginFailure(ctx context.Context, key string, now time.Time, window time.Duration) (LoginAttempts, error) {
	var out LoginAttempts
	err := s.Write(ctx, func(tx dbx.Tx) error {
		cur, err := readAttempts(ctx, tx, key)
		switch {
		case dbx.IsNoRows(err) || (err == nil && cur.WindowStart.Before(now.Add(-window))):
			out = LoginAttempts{Key: key, Count: 1, WindowStart: now.UTC().Truncate(time.Millisecond)}
		case err != nil:
			return err
		default:
			out = LoginAttempts{Key: key, Count: cur.Count + 1, WindowStart: cur.WindowStart}
		}
		_, err = tx.Exec(ctx, sqlRecordLoginFailure.SQL,
			key, out.Count, out.WindowStart)
		return err
	})
	if err != nil {
		return LoginAttempts{}, err
	}
	return out, nil
}

func (s *Store) ClearLoginAttemptsPrefix(ctx context.Context, prefix string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlClearLoginAttemptsPrefix.SQL, len(prefix), prefix)
		return err
	})
}

func (s *Store) ClearLoginAttempts(ctx context.Context, key string) error {
	return s.Write(ctx, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, sqlClearLoginAttempts.SQL, key)
		return err
	})
}
