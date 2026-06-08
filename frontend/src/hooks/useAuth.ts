"use client";
import { useState, useEffect, useCallback } from "react";
import { auth } from "@/lib/api";

export function useAuth() {
  const [signedIn, setSignedIn] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    auth.me()
      .then(() => setSignedIn(true))
      .catch(() => setSignedIn(false))
      .finally(() => setLoading(false));
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    await auth.signIn(email, password);
    setSignedIn(true);
  }, []);

  const signUp = useCallback(async (email: string, password: string, org: string) => {
    await auth.signUp(email, password, org);
    setSignedIn(true);
  }, []);

  const signOut = useCallback(async () => {
    await auth.signOut();
    setSignedIn(false);
  }, []);

  return { signedIn, loading, signIn, signUp, signOut };
}
