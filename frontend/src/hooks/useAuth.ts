"use client";
import { useState, useEffect, useCallback } from "react";
import { auth } from "@/lib/api";

// TODO: Swap localStorage for real JWT / session when backend is ready.
export function useAuth() {
  const [signedIn, setSignedIn] = useState(false);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const token = localStorage.getItem("agentmesh_token");
    if (!token) { setLoading(false); return; }
    auth.me()
      .then(() => setSignedIn(true))
      .catch(() => {
        localStorage.removeItem("agentmesh_signed_in");
        localStorage.removeItem("agentmesh_token");
        setSignedIn(false);
      })
      .finally(() => setLoading(false));
  }, []);

  const signIn = useCallback(async (email: string, password: string) => {
    const { token } = await auth.signIn(email, password);
    localStorage.setItem("agentmesh_signed_in", "1");
    localStorage.setItem("agentmesh_token", token);
    setSignedIn(true);
  }, []);

  const signUp = useCallback(async (email: string, password: string, org: string) => {
    const { token } = await auth.signUp(email, password, org);
    localStorage.setItem("agentmesh_signed_in", "1");
    localStorage.setItem("agentmesh_token", token);
    setSignedIn(true);
  }, []);

  const signOut = useCallback(async () => {
    await auth.signOut();
    localStorage.removeItem("agentmesh_signed_in");
    localStorage.removeItem("agentmesh_token");
    setSignedIn(false);
  }, []);

  return { signedIn, loading, signIn, signUp, signOut };
}
