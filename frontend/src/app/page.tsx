"use client";
import { LandingPage } from "@/components/landing/LandingPage";
import { useAuth } from "@/hooks/useAuth";

export default function Home() {
  const { signedIn } = useAuth();
  return <LandingPage signedIn={signedIn} />;
}
