"use client";
import { useState, useEffect, useCallback, useRef } from "react";
import { auth, AuthCheckError, AuthUser, isConnectionFailure } from "@/lib/api";
import {
  IS_NATIVE,
  setAuthToken,
  getAuthToken,
  authReady,
} from "@/lib/nativeAuth";
import { resetCredits } from "@/lib/credits/store";

const UI_COOKIE = "agentmesh_ui";
const TTL = 60 * 60 * 24 * 7; // 7 days -- matches backend JWT TTL

function setUICookie() {
  document.cookie = `${UI_COOKIE}=1; Path=/; SameSite=Lax; Max-Age=${TTL}`;
}

function clearUICookie() {
  document.cookie = `${UI_COOKIE}=; Path=/; SameSite=Lax; Max-Age=0`;
}

// Thrown when the credentials were accepted but the device would not keep the
// session. A named type because AuthPage shows a deliberately generic message
// for everything else -- an auth screen must not echo arbitrary server or
// developer strings back at the user -- and this is the one failure whose text
// is written for them and has to reach them intact.
export class SessionPersistError extends Error {
  constructor(cause: unknown) {
    super(
      "Signed in, but this device could not store the session securely. Sign in again, or restart the app if it keeps happening.",
      { cause },
    );
    this.name = "SessionPersistError";
  }
}

// Hands a freshly issued session to the native shell, and refuses the sign-in
// if the device cannot keep it.
//
// The shell writes the token to Keystore-backed storage. That write can fail --
// a key invalidated by a device credential change is the documented case -- and
// it used to be logged and swallowed, which produced the worst available
// outcome: the UI said signed in, the session worked until the app was closed,
// and the next launch found no token and presented a sign-in screen with no
// explanation. Nothing anywhere told the user what had happened.
//
// So the session is rolled back and the failure is thrown, where AuthPage
// already renders it. Sign-in either persists or does not happen, which is the
// same rule clearToken applies in the other direction: signing out has to mean
// signed out everywhere.
//
// Dynamic and IS_NATIVE-guarded, as before: a browser build must not pull
// Capacitor in.
// Exported for its test: it is a plain async function with no React in it, and
// the hooks around it cannot be rendered here (no testing-library in this
// project, and vitest only collects .test.ts).
export async function persistNativeSession(token: string): Promise<void> {
  setAuthToken(token);
  try {
    const { shell } = await import("@/native");
    await shell.onSignedIn(token);
  } catch (err) {
    // Revoke server-side first, while the token is still attached to requests,
    // then drop it locally. A failure here must not mask the one being
    // reported, which is the reason the caller is being told to stop.
    await auth.signOut().catch(() => {});
    setAuthToken(null);
    throw new SessionPersistError(err);
  }
}

export function useAuth() {
  const [signedIn, setSignedIn] = useState(false);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<AuthUser | null>(null);
  // The last session check could not reach the server. Not the same as signed
  // out: nothing is cleared, and the check can be run again with retry().
  const [offline, setOffline] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // Goes up whenever the session itself changes: a sign-in, a sign-up, or a
  // sign-out. A check that started before the change is then known to be
  // describing the session from before it, and is ignored rather than applied
  // on top of it.
  const sessionEpoch = useRef(0);

  useEffect(() => {
    let cancelled = false;
    // The session as it stood when this check went out. AuthPage mounts this
    // hook and lets the form be submitted while its own check is still in
    // flight, so a "no session" answer can arrive after that sign-in has
    // succeeded. Acting on it then cleared the cookie the sign-in had just
    // written, and middleware sent the signed-in user back to /signin.
    const epoch = sessionEpoch.current;
    // The token this check is made with. A rejection is about this one only:
    // a sign-in can replace it before the answer arrives.
    let sent: string | null = null;
    // On native, wait for NativeBoot to finish restoring (or fail to
    // restore) the persisted token before asking who's signed in -- calling
    // auth.me() first would race it and 401 with no Authorization header
    // attached yet.
    authReady
      .then(() => {
        sent = getAuthToken();
        return auth.me();
      })
      .then((u) => {
        if (cancelled || epoch !== sessionEpoch.current) return;
        setUICookie();
        setOffline(false);
        setSignedIn(true);
        setUser(u);
      })
      .catch((err) => {
        // A session started or ended since: this answer describes a session
        // that is no longer the current one and must change nothing.
        if (cancelled || epoch !== sessionEpoch.current) return;
        // A check that never got an answer says nothing about the session.
        // Treating it as signed out sent a signed-in user to the sign-in
        // screen whenever the phone was offline or the server was down.
        if (isConnectionFailure(err)) {
          setOffline(true);
          return;
        }
        // The server has answered that this token is not a session. On the
        // phone it has to go, from memory and from the device: NativeBoot
        // treats any token it holds as a session, so a stale one left in
        // place let a tapped notification open a protected screen without
        // signing in, and the next launch restored it again.
        //
        // Only the token this check sent, and only while it is still the
        // current one. Clearing whatever is current instead let a check that
        // failed just before a sign-in -- AuthPage runs its own -- delete the
        // session that sign-in had just saved.
        const rejected = sent;
        if (
          IS_NATIVE &&
          rejected !== null &&
          err instanceof AuthCheckError &&
          (err.status === 401 || err.status === 403)
        ) {
          if (getAuthToken() === rejected) setAuthToken(null);
          void import("@/native")
            .then(({ shell }) => shell.onSessionRejected(rejected))
            .catch((e) =>
              console.error("native shell failed to clear a rejected token", e),
            );
        }
        clearUICookie();
        setOffline(false);
        setSignedIn(false);
        setUser(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  // Runs the session check again, for the offline screen's Retry.
  const retry = useCallback(() => {
    setLoading(true);
    setAttempt((n) => n + 1);
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    const token = await auth.signIn(email, password);
    // Inert on the web, where the token is null and the HttpOnly cookie is the
    // session. On native this throws rather than resolving if the device could
    // not keep the token, so the two lines below are not reached.
    if (token && IS_NATIVE) await persistNativeSession(token);
    sessionEpoch.current += 1;
    setUICookie();
    setSignedIn(true);
  }, []);

  const signUp = useCallback(
    async (email: string, password: string, name: string, org: string) => {
      const token = await auth.signUp(email, password, name, org);
      if (token && IS_NATIVE) await persistNativeSession(token);
      sessionEpoch.current += 1;
      setUICookie();
      setSignedIn(true);
    },
    [],
  );

  const clearLocalSession = useCallback(() => {
    sessionEpoch.current += 1;
    if (IS_NATIVE) {
      setAuthToken(null);
      // Logged for the mirror-image reason: a shared device that fails to
      // clear the persisted token would otherwise silently keep the old
      // user's session live in Keystore after the UI has already moved on.
      void import("@/native")
        .then(({ shell }) => shell.onSignedOut())
        .catch((err) =>
          console.error("native shell failed to clear sign-out", err),
        );
    }
    clearUICookie();
    // Balance and purchase history are module singletons that survive a
    // client-side route change, so they have to be dropped explicitly here --
    // otherwise the next account to sign in in this tab sees the previous
    // one's money until its own fetch lands. resetCredits also bumps the
    // store's epoch, which discards any refresh still in flight.
    resetCredits();
    setSignedIn(false);
    setUser(null);
  }, []);

  const signOut = useCallback(async () => {
    // The local teardown below runs in a finally: auth.signOut() is a network
    // call, and letting a failed request skip the cleanup would leave the
    // previous account's balance, purchase history and (on native) persisted
    // token live in a UI that has already moved on. Signing out locally is
    // the part that must not be optional -- the server-side cookie clear is
    // best-effort by comparison.
    try {
      await auth.signOut();
    } catch (err) {
      console.error(
        "sign-out request failed; clearing local session anyway",
        err,
      );
    } finally {
      clearLocalSession();
    }
  }, [clearLocalSession]);

  // Completes the post-OAuth onboarding prompt (or a later profile edit) —
  // updates the backend then reflects it locally so callers don't need a
  // full re-fetch just to clear needsOnboarding.
  const completeOnboarding = useCallback(async (name: string, org: string) => {
    const updated = await auth.updateProfile(name, org);
    setUser(updated);
  }, []);

  return {
    signedIn,
    loading,
    offline,
    retry,
    user,
    signIn,
    signUp,
    signOut,
    completeOnboarding,
  };
}
