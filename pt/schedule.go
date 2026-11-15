package pt

import (
	"hash/fnv"
	"time"

	"github.com/octarq-org/octarq-plugins/pt/adapter"
)

// maxJitter caps how far the scheduler may move a planned run inside its
// window. Many trackers are fine with one request; a burst from every tracker
// at the same minute is what gets an account flagged.
const maxJitter = 30 * time.Minute

// location returns the tracker's timezone, falling back to server local time.
func (t *PTTracker) location() *time.Location {
	if t.Timezone != "" {
		if loc, err := time.LoadLocation(t.Timezone); err == nil {
			return loc
		}
	}
	return time.Local
}

// windowAny reports an unrestricted sign-in window.
func (t *PTTracker) windowAny() bool {
	return t.SigninWindowStartMin == 0 && t.SigninWindowEndMin == 0
}

// windowBounds returns the window in minutes, with a crossed midnight already
// normalized so end > start.
func (t *PTTracker) windowBounds() (start, end int) {
	start, end = t.SigninWindowStartMin, t.SigninWindowEndMin
	if end <= start && !t.windowAny() {
		end += 24 * 60
	}
	return start, end
}

// inSigninWindow reports whether now is inside the tracker's window.
func inSigninWindow(t *PTTracker, now time.Time) bool {
	if t.windowAny() {
		return true
	}
	local := now.In(t.location())
	mins := local.Hour()*60 + local.Minute()
	start, end := t.windowBounds()
	if mins < start {
		mins += 24 * 60
	}
	return mins >= start && mins < end
}

// windowLength is the size of the window; an unrestricted window counts as a
// full day for jitter purposes.
func windowLength(t *PTTracker) time.Duration {
	if t.windowAny() {
		return 24 * time.Hour
	}
	start, end := t.windowBounds()
	return time.Duration(end-start) * time.Minute
}

// nextWindowOpen returns the instant the window opens after now.
func nextWindowOpen(t *PTTracker, now time.Time) time.Time {
	loc := t.location()
	local := now.In(loc)
	if t.windowAny() {
		return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, loc)
	}
	start, _ := t.windowBounds()
	start %= 24 * 60
	today := time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc)
	if today.After(now) {
		return today
	}
	return today.AddDate(0, 0, 1)
}

// jitterFor derives a stable per-tracker offset, so a restart does not reshuffle
// the plan and every instance agrees on it.
func jitterFor(id uint, max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte{byte(id), byte(id >> 8), byte(id >> 16), byte(id >> 24)})
	return time.Duration(h.Sum64() % uint64(max))
}

// planSigninAt returns the next sign-in attempt time.
func planSigninAt(t *PTTracker, now time.Time) time.Time {
	open := nextWindowOpen(t, now)
	jitterCap := windowLength(t)
	if jitterCap > maxJitter {
		jitterCap = maxJitter
	}
	at := open.Add(jitterFor(t.ID, jitterCap))
	end := open.Add(windowLength(t))
	if at.After(end) {
		at = end.Add(-time.Minute)
	}
	return at
}

// planStatsAt returns the next statistics refresh time, with ±10% jitter.
func planStatsAt(t *PTTracker, now time.Time) time.Time {
	hours := t.StatsIntervalHours
	if hours <= 0 {
		return now.Add(24 * time.Hour)
	}
	base := time.Duration(hours) * time.Hour
	spread := base / 10
	offset := time.Duration(0)
	if spread > 0 {
		// Center the jitter so the average interval stays at base.
		offset = jitterFor(t.ID, 2*spread) - spread
	}
	return now.Add(base + offset)
}

// retryDelay maps a failure count and outcome to the next attempt delay.
//
// "Not confirmed" is retried inside the same day because the site may simply
// have raced the request; a hard auth failure waits much longer, and a
// manual-action pause has no automatic retry at all.
func retryDelay(failures int, outcome string) time.Duration {
	switch adapter.Outcome(outcome) {
	case adapter.OutcomeNeedsManualAction:
		return 0
	case adapter.OutcomeLoginOKUnconfirmed:
		if failures <= 1 {
			return time.Hour
		}
		return 24 * time.Hour
	case adapter.OutcomeAuthFailed:
		return 6 * time.Hour
	case adapter.OutcomeNetworkError, adapter.OutcomeRateLimited:
		switch {
		case failures <= 1:
			return 15 * time.Minute
		case failures == 2:
			return time.Hour
		default:
			return 6 * time.Hour
		}
	default:
		switch {
		case failures <= 1:
			return time.Hour
		default:
			return 6 * time.Hour
		}
	}
}

// dayKey is the tracker-local calendar day, used for once-per-day sign-in.
func dayKey(t *PTTracker, now time.Time) string {
	return now.In(t.location()).Format("2006-01-02")
}

// sameDay reports whether a recorded attempt belongs to the tracker-local day
// of now.
func sameDay(t *PTTracker, recorded, now time.Time) bool {
	return dayKey(t, recorded) == dayKey(t, now)
}
