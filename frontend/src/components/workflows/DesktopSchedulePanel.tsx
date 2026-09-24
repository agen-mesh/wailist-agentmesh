"use client";
import { useMemo, useState } from "react";
import {
  cadenceToCron,
  cronToCadence,
  describeCadence,
  nextLocalRun,
  ordinal,
  type Cadence,
  type CadenceValue,
} from "@/lib/cronCadence";

// The desktop version of RowMenu's schedule view. Compact viewports and the
// native shell keep the original SchedulePopover; this one has the room for
// a day grid, a live preview of the next run and a proper footer, so it is a
// separate tree rather than the same one restyled. Styles live in
// app/desktop.css under the `.sp-` prefix.

const CADENCES: Cadence[] = ["daily", "weekly", "monthly"];
// Monday-first, the way most calendars read; values are still 0 (Sun)-6 (Sat).
const WEEK = [
  { dow: 1, short: "Mon" },
  { dow: 2, short: "Tue" },
  { dow: 3, short: "Wed" },
  { dow: 4, short: "Thu" },
  { dow: 5, short: "Fri" },
  { dow: 6, short: "Sat" },
  { dow: 0, short: "Sun" },
];
const TIME_PRESETS = ["06:00", "09:00", "12:00", "18:00"];

const DEFAULTS: CadenceValue = {
  cadence: "daily",
  time: "09:00",
  dayOfWeek: 1,
  dayOfMonth: 1,
};

function localTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "local time";
  } catch {
    return "local time";
  }
}

function formatRun(d: Date): string {
  return d.toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function relative(d: Date, now: Date): string {
  const mins = Math.round((d.getTime() - now.getTime()) / 60_000);
  if (mins < 60) return `in ${Math.max(1, mins)} min`;
  const hours = Math.round(mins / 60);
  if (hours < 36) return `in ${hours} h`;
  return `in ${Math.round(hours / 24)} days`;
}

function Header({ onBack, title }: { onBack: () => void; title: string }) {
  return (
    <div className="sp-head">
      <button
        type="button"
        className="sp-back"
        onClick={onBack}
        aria-label="Back to menu"
      >
        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
          <path
            d="M8.5 3 4.5 7l4 4"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          />
        </svg>
      </button>
      <span className="sp-title">{title}</span>
    </div>
  );
}

export function DesktopSchedulePanel({
  loading,
  fetchError,
  onRetry,
  scheduleCron,
  scheduleNextRunAt,
  onBack,
  onCancel,
  onSave,
  onRemove,
}: {
  loading: boolean;
  fetchError: string | null;
  onRetry: () => void;
  scheduleCron?: string;
  scheduleNextRunAt?: string;
  onBack: () => void;
  onCancel: () => void;
  onSave: (cron: string) => Promise<void>;
  onRemove: () => Promise<void>;
}) {
  if (loading) {
    return (
      <div className="sp">
        <Header onBack={onBack} title="Schedule" />
        <div className="sp-body">
          <div className="sp-skeleton" />
          <div className="sp-skeleton" style={{ width: "60%" }} />
          <div className="sp-skeleton" style={{ height: 64 }} />
        </div>
      </div>
    );
  }
  if (fetchError) {
    // Never fall back to a form seeded with defaults here: Save would then
    // overwrite a real schedule we simply failed to read (see RowMenu).
    return (
      <div className="sp">
        <Header onBack={onBack} title="Schedule" />
        <div className="sp-body">
          <div className="sp-alert" role="alert">
            Couldn&apos;t load the schedule: {fetchError}
          </div>
        </div>
        <div className="sp-foot">
          <span />
          <div className="sp-foot-actions">
            <button type="button" className="sp-btn" onClick={onCancel}>
              Close
            </button>
            <button
              type="button"
              className="sp-btn sp-btn-primary"
              onClick={onRetry}
            >
              Retry
            </button>
          </div>
        </div>
      </div>
    );
  }
  return (
    <ScheduleForm
      scheduleCron={scheduleCron}
      scheduleNextRunAt={scheduleNextRunAt}
      onBack={onBack}
      onCancel={onCancel}
      onSave={onSave}
      onRemove={onRemove}
    />
  );
}

function ScheduleForm({
  scheduleCron,
  scheduleNextRunAt,
  onBack,
  onCancel,
  onSave,
  onRemove,
}: {
  scheduleCron?: string;
  scheduleNextRunAt?: string;
  onBack: () => void;
  onCancel: () => void;
  onSave: (cron: string) => Promise<void>;
  onRemove: () => Promise<void>;
}) {
  const initial = useMemo<CadenceValue>(
    () =>
      (scheduleCron ? cronToCadence(scheduleCron) : null) ?? { ...DEFAULTS },
    [scheduleCron],
  );
  const [cadence, setCadence] = useState<Cadence>(initial.cadence);
  const [time, setTime] = useState(initial.time);
  const [dayOfWeek, setDayOfWeek] = useState(initial.dayOfWeek ?? 1);
  const [dayOfMonth, setDayOfMonth] = useState(initial.dayOfMonth ?? 1);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [removeConfirming, setRemoveConfirming] = useState(false);

  const value: CadenceValue = { cadence, time, dayOfWeek, dayOfMonth };
  const timeValid = /^\d{2}:\d{2}$/.test(time);

  // Built on every render so an unschedulable monthly time is flagged while
  // the user is still picking it, not only after they press Save.
  let cron: string | null = null;
  let cronError: string | null = null;
  if (timeValid) {
    try {
      cron = cadenceToCron(value);
    } catch (e) {
      cronError = e instanceof Error ? e.message : "invalid schedule";
    }
  }

  const unchanged = !!scheduleCron && cron === scheduleCron;
  const now = new Date();
  const next = timeValid && !cronError ? nextLocalRun(value, now) : null;
  const tz = localTimeZone();

  const save = async () => {
    if (!cron) return;
    setSaving(true);
    setError(null);
    try {
      await onSave(cron);
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not save schedule");
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!removeConfirming) {
      setRemoveConfirming(true);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      await onRemove();
    } catch (e) {
      setError(e instanceof Error ? e.message : "could not remove schedule");
      setSaving(false);
      setRemoveConfirming(false);
    }
  };

  return (
    <form
      className="sp"
      onSubmit={(e) => {
        e.preventDefault();
        if (!unchanged) void save();
      }}
    >
      <Header
        onBack={onBack}
        title={scheduleCron ? "Edit schedule" : "Schedule"}
      />

      {scheduleCron && (
        <div className="sp-current">
          <span className="sp-dot" aria-hidden="true" />
          <span>
            Active
            {scheduleNextRunAt && (
              <> &middot; next {formatRun(new Date(scheduleNextRunAt))}</>
            )}
          </span>
        </div>
      )}

      <div className="sp-body">
        <div className="sp-seg" role="radiogroup" aria-label="Repeat">
          {CADENCES.map((c) => (
            <button
              key={c}
              type="button"
              role="radio"
              aria-checked={cadence === c}
              className="sp-seg-item"
              data-active={cadence === c || undefined}
              onClick={() => setCadence(c)}
            >
              {c}
            </button>
          ))}
        </div>

        {cadence === "weekly" && (
          <div className="sp-field">
            <span className="sp-label">Day</span>
            <div className="sp-days" role="radiogroup" aria-label="Day of week">
              {WEEK.map(({ dow, short }) => (
                <button
                  key={dow}
                  type="button"
                  role="radio"
                  aria-checked={dayOfWeek === dow}
                  className="sp-chip"
                  data-active={dayOfWeek === dow || undefined}
                  onClick={() => setDayOfWeek(dow)}
                >
                  {short}
                </button>
              ))}
            </div>
          </div>
        )}

        {cadence === "monthly" && (
          <div className="sp-field">
            <span className="sp-label">
              Day of month
              <span className="sp-label-hint">{ordinal(dayOfMonth)}</span>
            </span>
            <div
              className="sp-month"
              role="radiogroup"
              aria-label="Day of month"
            >
              {Array.from({ length: 28 }, (_, i) => i + 1).map((d) => (
                <button
                  key={d}
                  type="button"
                  role="radio"
                  aria-checked={dayOfMonth === d}
                  className="sp-cell"
                  data-active={dayOfMonth === d || undefined}
                  onClick={() => setDayOfMonth(d)}
                >
                  {d}
                </button>
              ))}
            </div>
            <span className="sp-help">
              Days 29&ndash;31 aren&apos;t offered: not every month has them.
            </span>
          </div>
        )}

        <div className="sp-field">
          <label className="sp-label" htmlFor="sp-time">
            Time
            <span className="sp-label-hint">{tz}</span>
          </label>
          <div className="sp-time-row">
            <input
              id="sp-time"
              type="time"
              className="sp-input"
              value={time}
              required
              onChange={(e) => setTime(e.target.value)}
            />
            <div className="sp-presets">
              {TIME_PRESETS.map((t) => (
                <button
                  key={t}
                  type="button"
                  className="sp-preset"
                  data-active={time === t || undefined}
                  onClick={() => setTime(t)}
                >
                  {t}
                </button>
              ))}
            </div>
          </div>
        </div>

        <div className="sp-preview" data-invalid={!!cronError || undefined}>
          {cronError ? (
            <span className="sp-preview-error">{cronError}</span>
          ) : next ? (
            <>
              <span className="sp-preview-main">{describeCadence(value)}</span>
              <span className="sp-preview-sub">
                Next run {formatRun(next)} &middot; {relative(next, now)}
              </span>
              {cron && (
                <span className="sp-preview-cron" title="Stored in UTC">
                  <code>{cron}</code> UTC
                </span>
              )}
            </>
          ) : (
            <span className="sp-preview-sub">Pick a time.</span>
          )}
        </div>

        <span className="sp-help">
          Runs are stored in UTC, so a run can shift by an hour when daylight
          saving starts or ends.
        </span>

        {error && (
          <div className="sp-alert" role="alert">
            {error}
          </div>
        )}
      </div>

      <div className="sp-foot">
        {scheduleCron ? (
          <button
            type="button"
            className="sp-btn sp-btn-danger"
            data-armed={removeConfirming || undefined}
            disabled={saving}
            onClick={remove}
            onBlur={() => setRemoveConfirming(false)}
          >
            {removeConfirming ? "Confirm remove" : "Remove"}
          </button>
        ) : (
          <span />
        )}
        <div className="sp-foot-actions">
          <button
            type="button"
            className="sp-btn"
            disabled={saving}
            onClick={onCancel}
          >
            Cancel
          </button>
          <button
            type="submit"
            className="sp-btn sp-btn-primary"
            disabled={saving || !cron || unchanged}
            title={unchanged ? "No changes" : undefined}
          >
            {saving ? "Saving…" : scheduleCron ? "Update" : "Save schedule"}
          </button>
        </div>
      </div>
    </form>
  );
}
