// Package jobs schedules and runs workspace jobs (brief <jobs_and_scheduler>).
// Jobs and their runs live in the workspace DB (D14); the admin DB keeps a
// derived _adm_job_index the scheduler sleeps on.
package jobs

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"abidata/internal/fault"
)

// parser accepts 5 fields plus descriptors (@hourly, @daily, @every 5m, …).
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// minEvery is the shortest @every interval accepted.
const minEvery = time.Minute

// Schedule computes fire times for a cron spec in a time zone.
//
// DST (robfig/cron v3 semantics, pinned by tests): a wall-clock time skipped by
// a spring-forward transition does not fire that day; a wall-clock time
// repeated by a fall-back transition fires twice, once in each offset. Jobs that
// must not run twice should avoid 01:00–01:59 local time in zones with DST, or
// schedule in UTC.
type Schedule struct {
	spec  string
	sched cron.Schedule
}

// ParseSchedule validates spec and binds it to tz (an IANA name).
func ParseSchedule(spec, tz string) (*Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fault.New(fault.BadRequest, "The schedule is empty.")
	}
	if strings.HasPrefix(strings.ToUpper(spec), "TZ=") || strings.HasPrefix(strings.ToUpper(spec), "CRON_TZ=") {
		return nil, fault.New(fault.BadRequest, "Set the job's time zone instead of a TZ= prefix in the schedule.")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" || strings.EqualFold(tz, "local") {
		return nil, fault.Newf(fault.BadRequest, "Unknown time zone %q.", tz)
	}
	s, err := parser.Parse(spec)
	if err != nil {
		return nil, fault.Newf(fault.BadRequest, "Invalid schedule %q: %s.", spec, err.Error())
	}
	switch c := s.(type) {
	case *cron.SpecSchedule:
		c.Location = loc
	case cron.ConstantDelaySchedule:
		if c.Delay < minEvery {
			return nil, fault.Newf(fault.BadRequest, "@every must be at least %s.", minEvery)
		}
	default:
		return nil, fmt.Errorf("jobs: unexpected schedule type %T", s)
	}
	return &Schedule{spec: spec, sched: s}, nil
}

// Next returns the first fire time strictly after t (UTC), or the zero time if none.
func (s *Schedule) Next(t time.Time) time.Time {
	n := s.sched.Next(t)
	if n.IsZero() {
		return n
	}
	return n.UTC()
}

// String returns the spec.
func (s *Schedule) String() string { return s.spec }
