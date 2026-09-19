package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/agentmesh/backend/internal/respond"
)

const (
	defaultUpcomingLimit = 20
	maxUpcomingLimit     = 50
	defaultUpcomingPer   = 3
	maxUpcomingPer       = 10
)

type upcomingRun struct {
	WorkflowID   string    `json:"workflowId"`
	WorkflowName string    `json:"workflowName"`
	At           time.Time `json:"at"`
	Cron         string    `json:"cron"`
}

// ListUpcomingRuns returns the next scheduled runs across the user's
// workflows, soonest first: up to ?per occurrences of each schedule, cut to
// ?limit overall.
//
// The first occurrence of each is the stored schedule_next_run_at, the exact
// time the scheduler will claim, rather than one recomputed here. The rest
// follow it through the same standard cron parser SetSchedule and the
// scheduler use, all in UTC.
func (d *Deps) ListUpcomingRuns(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value(CtxUserID).(string)
	limit, ok := upcomingQueryInt(w, r, "limit", defaultUpcomingLimit, maxUpcomingLimit)
	if !ok {
		return
	}
	per, ok := upcomingQueryInt(w, r, "per", defaultUpcomingPer, maxUpcomingPer)
	if !ok {
		return
	}

	wfs, err := d.Store.ListScheduledWorkflows(r.Context(), userID)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	upcoming := []upcomingRun{}
	for _, wf := range wfs {
		if wf.ScheduleCron == nil || wf.ScheduleNextRunAt == nil {
			continue
		}
		sched, err := cron.ParseStandard(*wf.ScheduleCron)
		if err != nil {
			// The scheduler clears a schedule it cannot parse on its next
			// tick; until then there is nothing honest to show.
			continue
		}
		at := wf.ScheduleNextRunAt.UTC()
		for i := 0; i < per; i++ {
			upcoming = append(upcoming, upcomingRun{
				WorkflowID:   wf.ID,
				WorkflowName: wf.Name,
				At:           at,
				Cron:         *wf.ScheduleCron,
			})
			at = sched.Next(at)
		}
	}
	sort.SliceStable(upcoming, func(i, j int) bool {
		return upcoming[i].At.Before(upcoming[j].At)
	})
	if len(upcoming) > limit {
		upcoming = upcoming[:limit]
	}
	respond.JSON(w, http.StatusOK, map[string]any{"upcoming": upcoming})
}

// upcomingQueryInt reads an optional positive integer query parameter,
// answering 400 itself when it is malformed, as the run history endpoints
// do. Missing means def; anything above max is clamped to it.
func upcomingQueryInt(w http.ResponseWriter, r *http.Request, name string, def, max int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		respond.Error(w, http.StatusBadRequest, name+" must be a positive number")
		return 0, false
	}
	return min(n, max), true
}
