"use client";
import { useState } from "react";
import { PasswordField } from "@/components/ui/PasswordField";
import { MAX_BYTES, MIN_LENGTH, scorePassword } from "@/lib/password/strength";
import { auth, type AuthUser } from "@/lib/api";
import {
  FormStatus,
  ReadOnlyRow,
  formatDateUTC,
  SaveButton,
  SettingRow,
  SettingsSection,
  inputStyle,
  useSaveState,
} from "@/components/settings/ui";

// `user` is deliberately non-nullable: the form below seeds its state with
// useState, which ignores later prop changes, so mounting with a half-loaded
// user would strand blank inputs and let a save wipe the stored organisation.
// SettingsPage holds this back until /auth/me resolves — the type is what keeps
// that contract from being quietly dropped later.
export function AccountSection({
  user,
  onProfileSaved,
}: {
  user: AuthUser;
  onProfileSaved: (name: string, org: string) => Promise<void>;
}) {
  const [name, setName] = useState(user.name);
  const [org, setOrg] = useState(user.orgName);
  const {
    state: profileState,
    message: profileMessage,
    fail: failProfile,
    run: runProfile,
  } = useSaveState();

  const saveProfile = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) {
      failProfile("Name can't be empty.");
      return;
    }
    await runProfile(
      () => onProfileSaved(name.trim(), org.trim()),
      "Profile updated.",
    );
  };

  return (
    <>
      <SettingsSection
        id="account"
        title="Account"
        description="Your name and organisation appear in the top bar and on anything you share."
      >
        <form onSubmit={saveProfile} style={{ display: "grid", gap: 18 }}>
          <SettingRow label="Display name" htmlFor="set-name">
            <input
              id="set-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={64}
              placeholder="Ada Lovelace"
              style={inputStyle}
            />
          </SettingRow>
          <SettingRow
            label="Organisation"
            htmlFor="set-org"
            hint="Shown next to the logo. Leave blank for a personal workspace."
          >
            <input
              id="set-org"
              value={org}
              onChange={(e) => setOrg(e.target.value)}
              maxLength={64}
              placeholder="Acme Capital"
              style={inputStyle}
            />
          </SettingRow>
          <ReadOnlyRow label="Email" value={user.email} mono />
          {/* createdAt stays optional on AuthUser — a response cached from
              before the field existed won't carry it — so memberSince still
              handles undefined. */}
          <ReadOnlyRow
            label="Member since"
            value={formatDateUTC(user.createdAt)}
          />
          <SaveButton saving={profileState === "saving"} />
          <FormStatus state={profileState} message={profileMessage} />
        </form>
      </SettingsSection>

      <PasswordSection />
    </>
  );
}

// Split out so a failed password change never clears the profile form's state,
// and so the password rules get their own explanation rather than being
// crammed into the account panel.
function StrengthMeter({ pw }: { pw: string }) {
  const { score, label, classes } = scorePassword(pw);
  // No --ok token exists, so the ramp runs danger -> warm -> accent.
  const colour =
    score >= 4 ? "var(--accent)" : score >= 2 ? "var(--warm)" : "var(--danger)";

  return (
    <div style={{ display: "grid", gap: 6, marginTop: 8 }}>
      <div style={{ display: "flex", gap: 4 }} aria-hidden>
        {[0, 1, 2, 3].map((i) => (
          <span
            key={i}
            className="pw-seg"
            style={{ background: i < score ? colour : "var(--border)" }}
          />
        ))}
      </div>
      {/* Says what it actually measures. This cannot know whether a password is
          common or breached, so it must not imply it does -- and it never uses
          the word "secure". */}
      <span
        aria-live="polite"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          letterSpacing: "0.04em",
          color: "var(--fg-muted)",
        }}
      >
        {pw
          ? `${label} · ${pw.length} characters, ${classes}/4 character types`
          : "Rated on length and variety only"}
      </span>
    </div>
  );
}

function Requirement({ met, children }: { met: boolean; children: string }) {
  return (
    <li
      className="pw-req"
      data-met={met}
      style={{
        display: "flex",
        alignItems: "center",
        gap: 8,
        color: met ? "var(--accent)" : "var(--fg-dim)",
        fontSize: 12,
      }}
    >
      <span aria-hidden style={{ fontFamily: "var(--font-mono)" }}>
        {met ? "✓" : "○"}
      </span>
      {children}
      {/* The glyph is decorative, so the state is carried as text for screen
          readers rather than left to colour alone. */}
      <span
        style={{
          position: "absolute",
          width: 1,
          height: 1,
          overflow: "hidden",
          clipPath: "inset(50%)",
        }}
      >
        {met ? " — met" : " — not met"}
      </span>
    </li>
  );
}

function PasswordSection() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const { state, message, fail, run } = useSaveState();

  const { meetsMinimum, tooLong } = scorePassword(next);
  const matches = next.length > 0 && next === confirm;
  // Mismatch is only worth showing once there is something to compare against,
  // so typing the confirmation does not flash an error on every keystroke.
  const showMismatch = confirm.length > 0 && next !== confirm;
  const canSubmit = Boolean(current) && meetsMinimum && !tooLong && matches;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    // The button is disabled unless these hold; kept as a guard because form
    // submission can still be triggered by Enter in some browsers.
    if (!meetsMinimum) {
      fail(`New password must be at least ${MIN_LENGTH} characters.`);
      return;
    }
    // bcrypt's own limit, enforced by the server. Said here so a long
    // passphrase out of a password manager gets a straight answer rather than
    // the 500 the backend used to return for it.
    if (tooLong) {
      fail(`New password must be at most ${MAX_BYTES} bytes.`);
      return;
    }
    // Caught here rather than server-side: the backend has no reason to know
    // about a confirmation field, and a round-trip to say "they don't match"
    // is a worse experience than saying it immediately.
    if (next !== confirm) {
      fail("The new passwords don't match.");
      return;
    }
    // run() reports the server's own wording on failure, which distinguishes a
    // wrong current password from an OAuth-only account that has no password.
    await run(async () => {
      await auth.changePassword(current, next);
      setCurrent("");
      setNext("");
      setConfirm("");
    }, "Password changed.");
  };

  return (
    <SettingsSection
      id="password"
      title="Password"
      description="Changing your password does not sign you out on other devices — sessions can't be revoked yet."
    >
      <form onSubmit={submit} style={{ display: "grid", gap: 18 }}>
        <SettingRow label="Current password" htmlFor="set-pw-current">
          <PasswordField
            id="set-pw-current"
            autoComplete="current-password"
            value={current}
            onChange={setCurrent}
            style={inputStyle}
          />
        </SettingRow>

        <SettingRow label="New password" htmlFor="set-pw-new">
          <>
            <PasswordField
              id="set-pw-new"
              autoComplete="new-password"
              value={next}
              onChange={setNext}
              style={inputStyle}
              describedBy="set-pw-reqs"
            />
            <StrengthMeter pw={next} />
            {/* Requirements are shown up front and tick off as they are met, so
                the form never fails on submit for a rule it never displayed.
                Only the two the server actually enforces appear here. */}
            <ul
              id="set-pw-reqs"
              style={{
                position: "relative",
                listStyle: "none",
                margin: "10px 0 0",
                padding: 0,
                display: "grid",
                gap: 6,
              }}
            >
              <Requirement met={meetsMinimum}>
                {`At least ${MIN_LENGTH} characters`}
              </Requirement>
              {/* Only worth a line once it is actually in play -- listing a
                  72-byte ceiling next to an 8-character floor would read as a
                  narrow window rather than the practical non-issue it is. */}
              {tooLong && (
                <Requirement met={false}>
                  {`At most ${MAX_BYTES} bytes (long passphrases count accented
                    and emoji characters as more than one)`}
                </Requirement>
              )}
              <Requirement met={matches}>Both new passwords match</Requirement>
            </ul>
          </>
        </SettingRow>

        <SettingRow
          label="Confirm new password"
          htmlFor="set-pw-confirm"
          hint={showMismatch ? "These don't match yet." : undefined}
        >
          <PasswordField
            id="set-pw-confirm"
            autoComplete="new-password"
            value={confirm}
            onChange={setConfirm}
            style={
              showMismatch
                ? { ...inputStyle, borderColor: "var(--danger)" }
                : inputStyle
            }
          />
        </SettingRow>

        {/* Disabled until the enforced rules pass, so the button never invites a
            request the server is going to reject. */}
        <SaveButton saving={state === "saving"} disabled={!canSubmit}>
          Change password
        </SaveButton>
        <FormStatus state={state} message={message} />
      </form>
    </SettingsSection>
  );
}
