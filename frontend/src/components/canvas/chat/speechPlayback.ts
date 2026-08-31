"use client";

// Singleton coordinator over the browser's speechSynthesis, which can only
// speak one utterance at a time. Every ChatMessage's speaker button
// subscribes here instead of holding its own "am I speaking" state, so
// starting one message's playback automatically flips every other button
// back to idle -- no prop-drilling through ChatPane/ChatRail/CanvasPage.
//
// Coordination glue over a browser singleton, not pure logic -- same
// boundary the codebase already draws around ChatMessage/ChatPane
// themselves, so this has no test file (see speechText.ts for the pure,
// tested half of TTS).

type Listener = () => void;

let speakingId: string | null = null;
const listeners = new Set<Listener>();
let keepAliveTimer: ReturnType<typeof setInterval> | null = null;

function notify() {
  for (const l of listeners) l();
}

function clearKeepAlive() {
  if (keepAliveTimer !== null) {
    clearInterval(keepAliveTimer);
    keepAliveTimer = null;
  }
}

/**
 * Chrome silently stops a speechSynthesis utterance partway through past
 * roughly 15s on some platforms unless paused/resumed periodically. Only
 * runs while something is actually speaking, cleared on end/error/stop.
 */
function startKeepAlive() {
  clearKeepAlive();
  keepAliveTimer = setInterval(() => {
    window.speechSynthesis.pause();
    window.speechSynthesis.resume();
  }, 10_000);
}

function setSpeaking(id: string | null) {
  if (speakingId === id) return;
  speakingId = id;
  notify();
}

export function ttsSupported(): boolean {
  return typeof window !== "undefined" && "speechSynthesis" in window;
}

export function isSpeaking(id: string): boolean {
  return speakingId === id;
}

/** Registers a listener fired whenever the speaking id changes. Returns an unsubscribe. */
export function subscribe(cb: Listener): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

/** Stops whatever is currently speaking, if anything. */
export function stop(): void {
  if (!ttsSupported()) return;
  clearKeepAlive();
  window.speechSynthesis.cancel();
  setSpeaking(null);
}

export function speak(id: string, text: string): void {
  if (!ttsSupported() || text.trim() === "") return;

  // The API only ever plays one utterance at a time -- cancel whatever is
  // speaking before starting the next one. The canceled utterance's own
  // onend/onerror may still fire asynchronously after this; both handlers
  // below are guarded to only clear state for the utterance that owns it,
  // so a stale event from the canceled one can't wipe out the new one.
  window.speechSynthesis.cancel();
  clearKeepAlive();

  const utterance = new SpeechSynthesisUtterance(text);
  utterance.onstart = () => {
    setSpeaking(id);
    startKeepAlive();
  };
  const clearIfCurrent = () => {
    if (speakingId !== id) return;
    clearKeepAlive();
    setSpeaking(null);
  };
  utterance.onend = clearIfCurrent;
  utterance.onerror = clearIfCurrent;

  window.speechSynthesis.speak(utterance);
}
