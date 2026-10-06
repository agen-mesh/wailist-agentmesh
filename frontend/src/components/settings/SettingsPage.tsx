"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { Topbar } from "@/components/Topbar";
import { useAuth } from "@/hooks/useAuth";
import styles from "./SettingsPage.module.css";

export function SettingsPage() {
  const router = useRouter();
  const { user, loading, offline, retry, deleteAccount } = useAuth();
  const [confirmation, setConfirmation] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [cleanupIncomplete, setCleanupIncomplete] = useState(false);
  const submitting = useRef(false);

  useEffect(() => {
    if (!loading && !offline && !user && !submitting.current) {
      router.replace("/signin?next=%2Fsettings");
    }
  }, [loading, offline, user, router]);

  const canDelete = !loading && !offline && user?.hasPassword !== undefined &&
    confirmation === "DELETE" && (!user.hasPassword || password.length > 0);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!canDelete || submitting.current) return;
    submitting.current = true;
    setDeleting(true);
    setError("");
    try {
      const incomplete = await deleteAccount(confirmation, password);
      setPassword("");
      if (incomplete) setCleanupIncomplete(true);
      else router.replace("/signin");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not delete account. Try again.");
      setPassword("");
      submitting.current = false;
      setDeleting(false);
    }
  }

  return (
    <div className={`am-viewport ${styles.viewport}`}>
      <Topbar />
      <div className={styles.scroll}>
        <main className={styles.page}>
          <Link href="/account" className={styles.back}>Back to account</Link>
          <h1>Settings</h1>
          {cleanupIncomplete ? (
            <div role="alert" className={styles.card}>
              <h2>Account deleted</h2>
              <p>
                Your server account is deleted, but this device could not clear all local data.
                Clear this site&apos;s stored data in your browser, or clear the Android app&apos;s
                storage in system settings before using another account.
              </p>
              <Link href="/signin">Go to sign in</Link>
            </div>
          ) : loading ? <p role="status">Loading account...</p> : offline ? (
            <div role="alert">
              <p>Connect to the server to manage your account.</p>
              <button type="button" onClick={retry}>Retry</button>
            </div>
          ) : user && (
            <section aria-labelledby="delete-title" className={styles.card}>
              <h2 id="delete-title">Delete account</h2>
              <p>
                Permanently delete <strong>{user.email}</strong> and its workflows,
                run history, saved credentials and account records. This cannot be undone.
                Any remaining credit balance will be lost.
              </p>
              <p>Release all active machines in the Tendril console before deleting your account.</p>
              <p>
                <Link href="/delete-account">What gets deleted and how to get help</Link>
              </p>
              {user.hasPassword === undefined ? (
                <p role="alert">Could not verify account requirements. Reload this page to try again.</p>
              ) : (
                <form onSubmit={submit} aria-busy={deleting}>
                  <label htmlFor="delete-confirmation">Type DELETE to confirm</label>
                  <input
                    id="delete-confirmation"
                    value={confirmation}
                    onChange={(event) => setConfirmation(event.target.value)}
                    autoComplete="off"
                    spellCheck={false}
                    disabled={deleting}
                    required
                  />
                  {user.hasPassword && (
                    <>
                      <label htmlFor="delete-password">Current password</label>
                      <input
                        id="delete-password"
                        type="password"
                        autoComplete="current-password"
                        value={password}
                        onChange={(event) => setPassword(event.target.value)}
                        disabled={deleting}
                        required
                      />
                    </>
                  )}
                  {error && <p role="alert" className={styles.error}>{error}</p>}
                  <button type="submit" className={styles.deleteButton} disabled={!canDelete || deleting}>
                    {deleting ? "Deleting account..." : "Delete my account"}
                  </button>
                </form>
              )}
            </section>
          )}
        </main>
      </div>
    </div>
  );
}
