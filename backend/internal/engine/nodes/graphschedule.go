package nodes

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The builder's set_schedule tool.
//
// A timetable is a workflow-level setting, not a node, and until now the
// builder could only tell the user to go and set one. "Every morning at 9"
// is the single most common thing a workflow is asked to do, so a build for
// it ended with a manual trigger and a chore -- and the chore carried a trap:
// the schedule is stored in UTC, so "9 am" typed as 0 9 * * * fires at 2:30
// pm in India.
//
// The model names the time the way the user said it, in the user's own
// timezone, and the server does the conversion. The conversion mirrors the
// Workflows page's cadenceToCron (frontend/src/lib/cronCadence.ts) exactly --
// the same three cadences, the same anchoring on today's date -- so a
// schedule the builder set is one the Schedule picker can read back and
// edit. Keep the two in step.

// builderSchedule is what set_schedule decided during one build.
type builderSchedule struct {
	// loc is the user's timezone, from the browser. UTC when the client did
	// not send one or sent one this server does not know.
	loc *time.Location
	// cron is the pending change: nil leaves the workflow's schedule alone,
	// a pointer to "" clears it, anything else is the UTC cron to save.
	cron *string
}

func newBuilderSchedule(timeZone string) *builderSchedule {
	loc := time.UTC
	if tz := strings.TrimSpace(timeZone); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	return &builderSchedule{loc: loc}
}

var weekdayNames = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday, "friday": time.Friday,
	"saturday": time.Saturday,
}

// parseClock reads "HH:MM" in 24-hour time.
func parseClock(s string) (hour, minute int, ok bool) {
	h, m, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, 0, false
	}
	hour, err1 := strconv.Atoi(h)
	minute, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

// argInt reads an integer argument whether the model sent it as a number or
// as a string.
func argInt(args map[string]any, key string) (int, bool) {
	switch v := args[key].(type) {
	case float64:
		if v == float64(int(v)) {
			return int(v), true
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// cadenceCron converts a local-time cadence to the UTC cron the scheduler
// runs, anchored on now's date in loc. See cadenceToCron in cronCadence.ts,
// which this mirrors line for line.
func cadenceCron(cadence string, hour, minute int, dayOfWeek time.Weekday, dayOfMonth int, loc *time.Location, now time.Time) string {
	today := now.In(loc)
	local := time.Date(today.Year(), today.Month(), today.Day(), hour, minute, 0, 0, loc)
	switch cadence {
	case "weekly":
		local = local.AddDate(0, 0, (int(dayOfWeek)-int(local.Weekday())+7)%7)
	case "monthly":
		local = time.Date(today.Year(), today.Month(), dayOfMonth, hour, minute, 0, 0, loc)
	}
	utc := local.UTC()
	switch cadence {
	case "weekly":
		return fmt.Sprintf("%d %d * * %d", utc.Minute(), utc.Hour(), int(utc.Weekday()))
	case "monthly":
		// A shift across a month boundary would name a day in the wrong
		// month; the picked day (1-28) is valid in every month, so keep it.
		dom := utc.Day()
		if utc.Month() != local.Month() || utc.Year() != local.Year() {
			dom = dayOfMonth
		}
		return fmt.Sprintf("%d %d %d * *", utc.Minute(), utc.Hour(), dom)
	default:
		return fmt.Sprintf("%d %d * * *", utc.Minute(), utc.Hour())
	}
}

// set handles one set_schedule call and returns what the model is told.
func (s *builderSchedule) set(args map[string]any, now time.Time) (string, error) {
	cadence := strings.ToLower(strings.TrimSpace(argString(args, "cadence")))
	if cadence == "off" {
		empty := ""
		s.cron = &empty
		return "schedule removed: the workflow will only run when someone starts it. Tell the user.", nil
	}
	if cadence != "daily" && cadence != "weekly" && cadence != "monthly" {
		return "", fmt.Errorf("set_schedule: cadence must be daily, weekly, monthly or off, not %q -- for anything else (hourly, weekdays only, several times a day) tell the user to set it on the Workflows page", cadence)
	}
	hour, minute, ok := parseClock(argString(args, "time"))
	if !ok {
		return "", fmt.Errorf("set_schedule: time must be 24-hour HH:MM, such as 09:00 or 18:30, not %q", argString(args, "time"))
	}

	var dow time.Weekday
	dom := 1
	var when string
	switch cadence {
	case "weekly":
		name := strings.ToLower(strings.TrimSpace(argString(args, "day")))
		d, known := weekdayNames[name]
		if !known {
			return "", fmt.Errorf("set_schedule: a weekly schedule needs day, a weekday name such as monday, not %q", name)
		}
		dow = d
		when = "every " + d.String()
	case "monthly":
		n, known := argInt(args, "dayOfMonth")
		if !known || n < 1 || n > 28 {
			// 28, not 31: a day that does not exist in every month would
			// silently skip February, and the Schedule picker offers 1-28.
			return "", fmt.Errorf("set_schedule: a monthly schedule needs dayOfMonth between 1 and 28")
		}
		dom = n
		when = fmt.Sprintf("on day %d of every month", n)
	default:
		when = "every day"
	}

	expr := cadenceCron(cadence, hour, minute, dow, dom, s.loc, now)
	s.cron = &expr
	return fmt.Sprintf("schedule set: %s at %02d:%02d %s (saved as the UTC cron %q). "+
		"It only fires once the workflow is deployed, so tell the user to press Deploy, and tell them the time and timezone above so they can check it. "+
		"The workflow still starts from its manual trigger -- do not add another trigger.",
		when, hour, minute, zoneName(s.loc), expr), nil
}

func zoneName(loc *time.Location) string {
	if loc == time.UTC {
		return "UTC"
	}
	return loc.String()
}
