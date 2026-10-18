package files

import (
	"context"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/ids"
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
	pendingCut := dbx.FormatTime(now.Add(-PendingTTL))
	graceCut := dbx.FormatTime(now.Add(-UnreferencedGrace))
	nowS := dbx.FormatTime(now)
	lock := s.wsLock(l.ID())
	err := l.Write(ctx, workspace.Background, func(tx dbx.Tx) error {
		// A backup copying blobs holds the lock for reading; never wait for it
		// while holding the writer. Try again on the next pass.
		if !lock.TryLock() {
			return nil
		}
		defer lock.Unlock()
		// Files that lost their last reference without the trigger noticing
		// (should not happen) start their grace period now.
		if _, err := tx.Exec(ctx, sqlSweepBatch.SQL, nowS); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, sqlSweepBatch2.SQL, pendingCut, graceCut, sweepBatch)
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
				if err := tx.QueryRow(ctx, sqlSweepBatch3.SQL, graceCut, c.id, c.id).Scan(&named); err != nil {
					return err
				}
				if named {
					res.Skipped++
					continue
				}
			}
			if _, err := tx.Exec(ctx, sqlSweepBatch4.SQL, c.id); err != nil {
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
		done := map[string]bool{}
		for _, c := range gone {
			if done[c.sha] {
				continue
			}
			done[c.sha] = true
			var shared bool
			if err := tx.QueryRow(ctx, sqlUpload.SQL, c.sha).Scan(&shared); err != nil {
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
	err = l.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, sqlSweepOrphans.SQL)
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
	err = l.Write(ctx, workspace.Background, func(tx dbx.Tx) error {
		if !lock.TryLock() {
			return nil
		}
		defer lock.Unlock()
		for _, o := range orphans {
			// Re-check on the writer: a row may have adopted the blob meanwhile.
			var exists bool
			if err := tx.QueryRow(ctx, sqlUpload.SQL, o.SHA).Scan(&exists); err != nil {
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
