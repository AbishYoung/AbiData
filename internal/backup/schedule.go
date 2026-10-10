package backup

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"abidata/internal/admin"
)

// PolicySettingKey is the _adm_settings key holding the scheduled-backup policy.
const PolicySettingKey = "backup_policy"

// Policy configures scheduled backups (platform-wide).
type Policy struct {
	Enabled bool `json:"enabled"`
	HourUTC int  `json:"hour_utc" minimum:"0" maximum:"23" doc:"Scheduled backups run once a day from this hour (UTC)."`
}

// DefaultPolicy backs every workspace up daily from 02:00 UTC.
var DefaultPolicy = Policy{Enabled: true, HourUTC: 2}

// LoadPolicy reads the scheduled-backup policy (DefaultPolicy when unset).
func LoadPolicy(ctx context.Context, a *admin.Store) (Policy, error) {
	v, err := a.GetSetting(ctx, PolicySettingKey)
	if errors.Is(err, admin.ErrNotFound) {
		return DefaultPolicy, nil
	}
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal([]byte(v), &p); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// RunSchedule takes each active workspace's daily scheduled backup once the
// policy hour has passed, checking every interval until ctx ends. A restart
// catches up on the same day; a missed day is not backfilled.
func (s *Service) RunSchedule(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		s.ScheduledTick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ScheduledTick runs one pass of the schedule.
func (s *Service) ScheduledTick(ctx context.Context) {
	p, err := LoadPolicy(ctx, s.Admin)
	if err != nil {
		s.log().Warn("backup policy", "subsystem", "backup", "err", err)
		return
	}
	now := s.clock()
	if !p.Enabled || now.Hour() < p.HourUTC {
		return
	}
	today := now.Format("2006-01-02")
	all, err := s.Admin.ListWorkspaces(ctx)
	if err != nil {
		s.log().Warn("backup schedule: list workspaces", "subsystem", "backup", "err", err)
		return
	}
	for _, w := range all {
		if ctx.Err() != nil {
			return
		}
		if w.Status != admin.StatusActive {
			continue
		}
		bs, err := s.Admin.ListBackups(ctx, w.ID)
		if err != nil {
			s.log().Warn("backup schedule: list backups", "subsystem", "backup", "workspace_id", w.ID, "err", err)
			continue
		}
		done := false
		for _, b := range bs {
			if b.Kind == admin.BackupScheduled && b.CreatedAt.UTC().Format("2006-01-02") == today {
				done = true
				break
			}
		}
		if done {
			continue
		}
		l, err := s.Workspaces.Acquire(ctx, w.ID)
		if err != nil {
			s.log().Warn("backup schedule: open workspace", "subsystem", "backup", "workspace_id", w.ID, "err", err)
			continue
		}
		start := time.Now()
		b, err := s.Snapshot(ctx, l, admin.BackupScheduled, "system")
		l.Release()
		if err != nil {
			s.log().Error("scheduled backup failed", "subsystem", "backup", "workspace_id", w.ID, "err", err)
			continue
		}
		s.log().Info("scheduled backup", "subsystem", "backup", "workspace_id", w.ID, "backup_id", b.ID,
			"gz_bytes", b.GzBytes, "ms", time.Since(start).Milliseconds())
	}
}
