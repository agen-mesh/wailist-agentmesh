"use client";
import { useState, useEffect, useCallback } from "react";
import { auth } from "@/lib/api";

const UI_COOKIE = "agentmesh_ui";
const TTL = 60 * 60 * 24 * 7; // 7 days — matches backend JWT TTL

function setUICookie() {
  document.cookie = `${UI_COOKIE}=1; Path=/; SameSite=Lax; Max-Age=${TTL}`;
}

function clearUICookie() {
  document.cookie = `${UI_COOKIE}=; Path=/; SameSite=Lax; Max-Age=0`;
}

export function useAuth() {
  const [signedIn, setSignedIn] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    auth.me()
      .then(() => { setUICookie(); setSignedIn(true); })
      .catch(() => { clearUICookie(); setSignedIn(false); })
      .finally(() => setLoading(false));
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    await auth.signIn(email, password);
    setUICookie();
    setSignedIn(true);
  }, []);

  const signUp = useCallback(async (email: string, password: string, org: string) => {
    await auth.signUp(email, password, org);
    setUICookie();
    setSignedIn(true);
  }, []);

  const signOut = useCallback(async () => {
    await auth.signOut();
    clearUICookie();
    setSignedIn(false);
  }, []);

  return { signedIn, loading, signIn, signUp, signOut };
}
