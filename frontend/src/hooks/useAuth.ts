"use client";
import { useState, useEffect, useCallback } from "react";
import { auth, AuthUser } from "@/lib/api";

const UI_COOKIE = "agentmesh_ui";
const TTL = 60 * 60 * 24 * 7; // 7 days -- matches backend JWT TTL

function setUICookie() {
  document.cookie = `${UI_COOKIE}=1; Path=/; SameSite=Lax; Max-Age=${TTL}`;
}

function clearUICookie() {
  document.cookie = `${UI_COOKIE}=; Path=/; SameSite=Lax; Max-Age=0`;
}

export function useAuth() {
  const [signedIn, setSignedIn] = useState(false);
  const [loading, setLoading] = useState(true);
  const [user, setUser] = useState<AuthUser | null>(null);

  useEffect(() => {
    auth
      .me()
      .then((u) => {
        setUICookie();
        setSignedIn(true);
        setUser(u);
      })
      .catch(() => {
        clearUICookie();
        setSignedIn(false);
        setUser(null);
      })
      .finally(() => setLoading(false));
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    await auth.signIn(email, password);
    setUICookie();
    setSignedIn(true);
  }, []);

  const signUp = useCallback(
    async (email: string, password: string, name: string, org: string) => {
      await auth.signUp(email, password, name, org);
      setUICookie();
      setSignedIn(true);
    },
    [],
  );

  const signOut = useCallback(async () => {
    await auth.signOut();
    clearUICookie();
    setSignedIn(false);
    setUser(null);
  }, []);

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
    user,
    signIn,
    signUp,
    signOut,
    completeOnboarding,
  };
}
