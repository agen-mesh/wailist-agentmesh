package nodes

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A fixed winter instant, so DST zones have a known offset.
var schedNow = time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC)

func setSchedule(t *testing.T, tz string, args map[string]any) (*builderSchedule, string) {
	t.Helper()
	s := newBuilderSchedule(tz)
	msg, err := s.set(args, schedNow)
	if err != nil {
		t.Fatalf("set_schedule(%v): %v", args, err)
	}
	return s, msg
}

// The bug this exists for: "9 am" typed straight into a UTC cron fires at
// 2:30 pm in India.
func TestSetScheduleConvertsLocalTimeToUTC(t *testing.T) {
	cases := []struct {
		name, tz string
		args     map[string]any
		want     string
	}{
		{"india daily", "Asia/Kolkata", map[string]any{"cadence": "daily", "time": "09:00"}, "30 3 * * *"},
		{"utc daily", "UTC", map[string]any{"cadence": "daily", "time": "09:00"}, "0 9 * * *"},
		{"new york daily in winter", "America/New_York", map[string]any{"cadence": "daily", "time": "09:00"}, "0 14 * * *"},
		// Crosses back a day: Monday 08:00 in Tokyo is Sunday 23:00 UTC.
		{"tokyo weekly", "Asia/Tokyo", map[string]any{"cadence": "weekly", "time": "08:00", "day": "Monday"}, "0 23 * * 0"},
		// Crosses forward a day: Monday 20:00 in LA is Tuesday 04:00 UTC.
		{"los angeles weekly", "America/Los_Angeles", map[string]any{"cadence": "weekly", "time": "20:00", "day": "monday"}, "0 4 * * 2"},
		{"india monthly", "Asia/Kolkata", map[string]any{"cadence": "monthly", "time": "09:00", "dayOfMonth": float64(15)}, "30 3 15 * *"},
		// Day 1 at 02:00 in India is the last day of the previous month in
		// UTC; the picked day is kept rather than naming the wrong month.
		{"monthly across a month boundary", "Asia/Kolkata", map[string]any{"cadence": "monthly", "time": "02:00", "dayOfMonth": "1"}, "30 20 1 * *"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := setSchedule(t, c.tz, c.args)
			if s.cron == nil || *s.cron != c.want {
				t.Fatalf("cron = %v, want %q", s.cron, c.want)
			}
		})
	}
}

// The model has to be able to tell the user exactly what was set, in their
// own terms, and that nothing fires until the workflow is deployed.
func TestSetScheduleTellsTheModelWhatItSet(t *testing.T) {
	_, msg := setSchedule(t, "Asia/Kolkata", map[string]any{"cadence": "daily", "time": "09:00"})
	for _, want := range []string{"every day", "09:00", "Asia/Kolkata", "Deploy"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q is missing %q", msg, want)
		}
	}
}

// An unknown or missing zone must not silently become some other zone the
// user never named. UTC, stated as UTC, is the honest fallback.
func TestSetScheduleFallsBackToUTCAndSaysSo(t *testing.T) {
	for _, tz := range []string{"", "Not/AZone"} {
		s, msg := setSchedule(t, tz, map[string]any{"cadence": "daily", "time": "09:00"})
		if *s.cron != "0 9 * * *" {
			t.Errorf("tz %q: cron = %q", tz, *s.cron)
		}
		if !strings.Contains(msg, "UTC") {
			t.Errorf("tz %q: the message must say the time is UTC: %s", tz, msg)
		}
	}
}

func TestSetScheduleOffClearsIt(t *testing.T) {
	s, _ := setSchedule(t, "UTC", map[string]any{"cadence": "off"})
	if s.cron == nil || *s.cron != "" {
		t.Fatalf("cron = %v, want a pointer to the empty string", s.cron)
	}
}

// A build that never calls set_schedule must not touch an existing schedule.
func TestNoSetScheduleLeavesTheScheduleAlone(t *testing.T) {
	if s := newBuilderSchedule("Asia/Kolkata"); s.cron != nil {
		t.Fatalf("cron = %q, want nil", *s.cron)
	}
}

func TestSetScheduleRejectsWhatThePickerCannotShow(t *testing.T) {
	bad := []map[string]any{
		{"cadence": "hourly", "time": "09:00"},
		{"cadence": "daily", "time": "9am"},
		{"cadence": "daily", "time": "24:00"},
		{"cadence": "weekly", "time": "09:00"},
		{"cadence": "weekly", "time": "09:00", "day": "weekdays"},
		{"cadence": "monthly", "time": "09:00", "dayOfMonth": float64(31)},
		{"cadence": "monthly", "time": "09:00"},
	}
	for _, args := range bad {
		s := newBuilderSchedule("UTC")
		if _, err := s.set(args, schedNow); err == nil {
			t.Errorf("set_schedule(%v) was accepted", args)
		}
		if s.cron != nil {
			t.Errorf("set_schedule(%v) changed the schedule despite failing", args)
		}
	}
}

// Through the real build loop: the schedule the model set comes back on the
// result, converted with the timezone the request carried.
func TestBuildGraphReturnsTheScheduleTheModelSet(t *testing.T) {
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		turn++
		w.Header().Set("Content-Type", "application/json")
		switch turn {
		case 1:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"set_schedule","args":{"cadence":"daily","time":"09:00"}}}]}}]}`)
		default:
			io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Scheduled for 9 am every day. Deploy it to start."}]}}]}`)
		}
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), BuildRequest{
		APIKey: "k", Message: "run this every morning at 9", TimeZone: "Asia/Kolkata",
	})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if res.Schedule == nil {
		t.Fatal("the schedule the model set was dropped")
	}
	// Asia/Kolkata has no DST, so this holds whatever today's date is.
	if *res.Schedule != "30 3 * * *" {
		t.Errorf("schedule = %q, want 30 3 * * *", *res.Schedule)
	}
}

func TestBuildGraphWithoutSetScheduleLeavesItNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"Nothing to change."}]}}]}`)
	}))
	defer srv.Close()
	SetGeminiBaseURL(srv.URL)
	defer SetGeminiBaseURL("https://generativelanguage.googleapis.com")

	res, err := BuildGraph(context.Background(), BuildRequest{APIKey: "k", Message: "hi"})
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if res.Schedule != nil {
		t.Errorf("schedule = %q, want nil so an existing schedule is left alone", *res.Schedule)
	}
}
