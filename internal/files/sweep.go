package files

import (
	"context"
	"time"

	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// SweepResult reports one janitor pass over a workspace.
type SweepResult struct {
	PurgedPending      int   // never-attached files older than PendingTTL
	PurgedUnreferenced int   // files whose last reference ended UnreferencedGrace ago
	Skipped            int   // candidates kept because recent row history still names them
	Orphans            int   // blobs on disk that no row describes
	Bytes              int64 // bytes freed (blobs actually removed)
}

const sweepBatch = 500

// orphanMinAge keeps blobs that are still being adopted by an upload.
const orphanMinAge = 24 * time.Hour

// Sweep deletes expired files of the workspace: pending uploads older than
// PendingTTL, and attached files that have had no reference for
// UnreferencedGrace (counted from last_unreferenced_at). A file that recent row
// history still mentions is kept until that history is itself older than the
// grace period, so undo and "show history" never point at a purged file.
// Blobs are removed only when no other row shares the hash. now is a parameter
// so tests control the clock.
func (s *Service) Sweep(ctx context.Context, l *workspace.Lease, now time.Time) (SweepResult, error) {
	var res SweepResult
	bk, err := s.BucketFor(l.ID())
	if err != nil {
		return res, err
	}
	for range 20 { // bounded: at most 20 batches per pass
		batch, err := s.sweepBatch(ctx, l, bk, now)
		res.PurgedPending += batch.PurgedPending
		res.PurgedUnreferenced += batch.PurgedUnreferenced
		res.Skipped += batch.Skipped
		res.Bytes += batch.Bytes
		if err != nil {
			return res, err
		}
		if batch.PurgedPending+batch.PurgedUnreferenced == 0 || batch.PurgedPending+batch.PurgedUnreferenced+batch.Skipped < sweepBatch {
			break
		}
	}
	bk.SweepTemp(now, time.Hour)
	if s.dueOrphanScan(l.ID(), now) {
		n, b, err := s.sweepOrphans(ctx, l, bk, now)
		res.Orphans, res.Bytes = n, res.Bytes+b
		if err != nil {
			return res, err
		}
	}
	if s.OnPurge != nil && (res.PurgedPending+res.PurgedUnreferenced+res.Orphans) > 0 {
		s.OnPurge(res.PurgedPending+res.PurgedUnreferenced+res.Orphans, res.Bytes)
	}
	return res, nil
}

func (s *Service) dueOrphanScan(ws ids.WorkspaceID, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastScan == nil {
		s.lastScan = map[ids.WorkspaceID]time.Time{}
	}
	every := s.scanEvery
	if every == 0 {
		every = 24 * time.Hour
	}
	if last, ok := s.lastScan[ws]; ok && now.Sub(last) < every {
		return false
	}
	s.lastScan[ws] = now
	return true
}

type candidate struct {
	id, sha string
	pending bool
	size    int64
}

func (s *Service) sweepBatch(ctx context.Context, l *workspace.Lease, bk Bucket, now time.Time) (SweepResult, error) {
	var res SweepResult
	pendingCut := sqlitex.FormatTime(now.Add(-PendingTTL))
	graceCut := sqlitex.FormatTime(now.Add(-UnreferencedGrace))
	nowS := sqlitex.FormatTime(now)
	lock := s.wsLock(l.ID())
	err := l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		// Files that lost their last reference without the trigger noticing
		// (should not happen) start their grace period now.
		if _, err := tx.Exec(ctx, `UPDATE _sys_files SET last_unreferenced_at = ? WHERE state = 'attached' AND last_unreferenced_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = _sys_files.id)`, nowS); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, sha256, state, size FROM _sys_files
			WHERE (state = 'pending' AND created_at < ? AND NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = _sys_files.id))
			   OR (state = 'attached' AND last_unreferenced_at IS NOT NULL AND last_unreferenced_at < ?
			       AND NOT EXISTS (SELECT 1 FROM _sys_file_refs r WHERE r.file_id = _sys_files.id))
			ORDER BY created_at LIMIT ?`, pendingCut, graceCut, sweepBatch)
		if err != nil {
			return err
		}
		var cands []candidate
		for rows.Next() {
			var c candidate
			var state string
			if err := rows.Scan(&c.id, &c.sha, &state, &c.size); err != nil {
				rows.Close()
				return err
			}
			c.pending = state == "pending"
			cands = append(cands, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		var gone []candidate
		for _, c := range cands {
			if !c.pending {
				var named bool
				// ULIDs are unique, so a substring match on the JSON snapshots is exact.
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM _sys_row_history WHERE at >= ?
					AND (before LIKE '%' || ? || '%' OR after LIKE '%' || ? || '%'))`, graceCut, c.id, c.id).Scan(&named); err != nil {
					return err
				}
				if named {
					res.Skipped++
					continue
				}
			}
			if _, err := tx.Exec(ctx, `DELETE FROM _sys_files WHERE id = ?`, c.id); err != nil {
				return err
			}
			gone = append(gone, c)
			if c.pending {
				res.PurgedPending++
			} else {
				res.PurgedUnreferenced++
			}
		}
		if len(gone) == 0 {
			return nil
		}
		// Remove blobs no remaining row shares. This happens inside the writer
		// transaction: an upload adopting the same hash cannot interleave. If the
		// commit fails, the rows reappear and the next pass deletes them again.
		lock.Lock()
		defer lock.Unlock()
		done := map[string]bool{}
		for _, c := range gone {
			if done[c.sha] {
				continue
			}
			done[c.sha] = true
			var shared bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM _sys_files WHERE sha256 = ?)`, c.sha).Scan(&shared); err != nil {
				return err
			}
			if shared {
				continue
			}
			if err := bk.Remove(c.sha); err != nil {
				return err
			}
			res.Bytes += c.size
		}
		return nil
	})
	return res, err
}

// sweepOrphans removes blobs older than orphanMinAge that no _sys_files row
// describes (left by a crash between placing a blob and committing its row).
func (s *Service) sweepOrphans(ctx context.Context, l *workspace.Lease, bk Bucket, now time.Time) (n int, freed int64, err error) {
	known := map[string]bool{}
	err = l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT DISTINCT sha256 FROM _sys_files`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sha string
			if err := rows.Scan(&sha); err != nil {
				return err
			}
			known[sha] = true
		}
		return rows.Err()
	})
	if err != nil {
		return 0, 0, err
	}
	var orphans []BlobInfo
	if err := bk.Walk(func(b BlobInfo) error {
		if !known[b.SHA] && now.Sub(b.ModTime) > orphanMinAge {
			orphans = append(orphans, b)
		}
		return nil
	}); err != nil {
		return 0, 0, err
	}
	if len(orphans) == 0 {
		return 0, 0, nil
	}
	lock := s.wsLock(l.ID())
	err = l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		lock.Lock()
		defer lock.Unlock()
		for _, o := range orphans {
			// Re-check on the writer: a row may have adopted the blob meanwhile.
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM _sys_files WHERE sha256 = ?)`, o.SHA).Scan(&exists); err != nil {
				return err
			}
			if exists {
				continue
			}
			if err := bk.Remove(o.SHA); err != nil {
				return err
			}
			n++
			freed += o.Size
		}
		return nil
	})
	return n, freed, err
}
