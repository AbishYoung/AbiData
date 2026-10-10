package jobs

import (
	"testing"
	"time"

	"abidata/internal/fault"
)

func mustParse(t *testing.T, spec, tz string) *Schedule {
	t.Helper()
	s, err := ParseSchedule(spec, tz)
	if err != nil {
		t.Fatalf("%s %s: %v", spec, tz, err)
	}
	return s
}

func at(t *testing.T, tz, v string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		t.Fatal(err)
	}
	x, err := time.ParseInLocation("2006-01-02 15:04", v, loc)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// fires lists the next n fire times after start, formatted in tz.
func fires(s *Schedule, start time.Time, tz string, n int) []string {
	loc, _ := time.LoadLocation(tz)
	var out []string
	t := start
	for range n {
		t = s.Next(t)
		out = append(out, t.In(loc).Format("2006-01-02 15:04 MST"))
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] got %v\nwant %v", i, got, want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, c := range [][2]string{
		{"", "UTC"}, {"* * *", "UTC"}, {"61 * * * *", "UTC"}, {"* * * * * *", "UTC"}, // seconds field not accepted
		{"TZ=UTC * * * * *", "UTC"}, {"CRON_TZ=Asia/Tokyo 0 9 * * *", "UTC"},
		{"0 9 * * *", "Mars/Olympus"}, {"0 9 * * *", ""}, {"0 9 * * *", "Local"},
		{"@every 10s", "UTC"}, {"@yearly-ish", "UTC"},
	} {
		if _, err := ParseSchedule(c[0], c[1]); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("%q %q: %v", c[0], c[1], err)
		}
	}
}

func TestTimezones(t *testing.T) {
	// 09:00 every weekday in Tokyo, expressed in Tokyo time.
	s := mustParse(t, "0 9 * * 1-5", "Asia/Tokyo")
	eq(t, fires(s, at(t, "Asia/Tokyo", "2026-10-09 10:00"), "Asia/Tokyo", 3), []string{
		"2026-10-12 09:00 JST", "2026-10-13 09:00 JST", "2026-10-14 09:00 JST",
	})
	// Results are UTC.
	if n := s.Next(at(t, "Asia/Tokyo", "2026-10-12 08:00")); n.Location() != time.UTC || n.Hour() != 0 {
		t.Fatalf("not UTC: %v", n)
	}
	// Descriptors and @every.
	d := mustParse(t, "@daily", "America/New_York")
	eq(t, fires(d, at(t, "America/New_York", "2026-10-09 10:00"), "America/New_York", 2), []string{"2026-10-10 00:00 EDT", "2026-10-11 00:00 EDT"})
	e := mustParse(t, "@every 15m", "UTC")
	eq(t, fires(e, at(t, "UTC", "2026-10-09 10:07"), "UTC", 2), []string{"2026-10-09 10:22 UTC", "2026-10-09 10:37 UTC"})
	// Month-end and leap day.
	m := mustParse(t, "0 0 29 2 *", "UTC")
	eq(t, fires(m, at(t, "UTC", "2026-03-01 00:00"), "UTC", 2), []string{"2028-02-29 00:00 UTC", "2032-02-29 00:00 UTC"})
}

func TestDST(t *testing.T) {
	const ny = "America/New_York"
	// Spring forward 2027-03-14: 02:00 → 03:00. 02:30 does not exist and is skipped that day.
	s := mustParse(t, "30 2 * * *", ny)
	eq(t, fires(s, at(t, ny, "2027-03-13 12:00"), ny, 2), []string{"2027-03-15 02:30 EDT", "2027-03-16 02:30 EDT"})
	// Hourly across spring forward: no 02:xx run, 03:00 follows 01:00 one real hour later.
	h := mustParse(t, "0 * * * *", ny)
	eq(t, fires(h, at(t, ny, "2027-03-14 00:30"), ny, 3), []string{"2027-03-14 01:00 EST", "2027-03-14 03:00 EDT", "2027-03-14 04:00 EDT"})
	// Fall back 2026-11-01: 01:00–02:00 repeats, and 01:30 fires in both offsets.
	f := mustParse(t, "30 1 * * *", ny)
	eq(t, fires(f, at(t, ny, "2026-10-31 12:00"), ny, 3), []string{"2026-11-01 01:30 EDT", "2026-11-01 01:30 EST", "2026-11-02 01:30 EST"})
	// Daily at 09:00 keeps wall-clock time across both transitions.
	d := mustParse(t, "0 9 * * *", ny)
	eq(t, fires(d, at(t, ny, "2026-10-31 10:00"), ny, 2), []string{"2026-11-01 09:00 EST", "2026-11-02 09:00 EST"})
}
