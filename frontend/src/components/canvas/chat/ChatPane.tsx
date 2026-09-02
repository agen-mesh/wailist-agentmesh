"use client";
import { useEffect, useRef, useState } from "react";
import { ChatMessage } from "./ChatMessage";
import type { ChatSession } from "./useChatSession";

// Composer sizing. Three lines at rest so a typical prompt is visible as a
// whole rather than through a one-line slot, growing to roughly ten before it
// starts scrolling.
const COMPOSER_MIN_H = 76;
const COMPOSER_MAX_H = 220;

// The human half of the console: transcript above, composer below.
//
// Deliberately self-sufficient. Someone who never opens the Logs pane should
// still be able to run the workflow, read the answer, and see what it cost --
// that is the whole "not important for normal people" requirement.

interface ChatPaneProps {
  session: ChatSession;
  /** Sends a message; the parent turns it into a run. */
  onSend: (text: string) => void;
  /** True while a run is in flight — the composer waits rather than queueing. */
  busy: boolean;
  onShowLogs?: () => void;
}

export function ChatPane({ session, onSend, busy, onShowLogs }: ChatPaneProps) {
  const [draft, setDraft] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  // The composer starts three lines tall and grows with the draft up to
  // COMPOSER_MAX_H, after which it scrolls. Height has to be reset to "auto"
  // before each measurement -- scrollHeight never shrinks below the element's
  // current height, so without the reset the box could only ever grow.
  useEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(
      Math.max(el.scrollHeight, COMPOSER_MIN_H),
      COMPOSER_MAX_H,
    )}px`;
  }, [draft]);

  // Follow the conversation as it grows, the same way the log list does.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [session.messages]);

  const submit = () => {
    const text = draft.trim();
    if (!text || busy) return;
    setDraft("");
    onSend(text);
  };

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        // flex:1 against the wrapper's explicit column, rather than a
        // percentage height against an indefinite parent.
        flex: 1,
        minHeight: 0,
        // A flex item defaults to min-width:auto, i.e. it refuses to shrink
        // below its own min-content width. The composer's textarea carries an
        // intrinsic ~20-column width, which floored this pane at ~289px and
        // pushed the bubbles and the Send button past the rail's right edge
        // once the rail was dragged near its 260px minimum.
        minWidth: 0,
      }}
    >
      {/* Transcript */}
      <div
        role="log"
        aria-live="polite"
        aria-label="Conversation"
        className="chat-transcript"
        style={{
          flex: 1,
          minHeight: 0,
          minWidth: 0,
          overflow: "auto",
          padding: "10px 14px",
          display: "flex",
          flexDirection: "column",
          gap: 12,
        }}
      >
        {session.hydrated && session.messages.length === 0 && (
          <div
            style={{
              margin: "auto",
              textAlign: "center",
              maxWidth: "34ch",
              color: "var(--fg-dim)",
              fontSize: 12,
              lineHeight: 1.6,
            }}
          >
            Ask this workflow something to run it.
            <span
              style={{
                display: "block",
                marginTop: 8,
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                lineHeight: 1.7,
              }}
            >
              {/* Honest about the model we actually ship: every message is an
                  independent run, exactly as n8n's chat trigger behaves
                  without a memory node. Better to say so than to imply a
                  continuity the engine does not have. */}
              each message runs the workflow fresh — agents don&apos;t remember
              previous messages yet
            </span>
          </div>
        )}

        {session.messages.map((m) => (
          <ChatMessage key={m.id} message={m} onShowLogs={onShowLogs} />
        ))}
        <div ref={bottomRef} />
      </div>

      {/* Composer */}
      <div
        style={{
          flexShrink: 0,
          minWidth: 0,
          borderTop: "1px solid var(--border)",
          padding: 10,
          display: "flex",
          gap: 8,
          alignItems: "flex-end",
        }}
      >
        <textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            // Enter sends, Shift+Enter starts a new line -- the convention
            // every chat UI uses, including n8n's.
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          ref={inputRef}
          rows={3}
          placeholder={busy ? "Running…" : "Ask something…"}
          disabled={busy}
          style={{
            flex: 1,
            // Without this the textarea's intrinsic column width becomes a
            // hard floor and the Send button gets pushed out of the rail.
            minWidth: 0,
            resize: "none",
            minHeight: COMPOSER_MIN_H,
            maxHeight: COMPOSER_MAX_H,
            padding: "9px 11px",
            background: "var(--bg)",
            border: "1px solid var(--border)",
            borderRadius: "var(--r-2)",
            color: "var(--fg)",
            fontFamily: "var(--font-sans)",
            fontSize: 13,
            lineHeight: 1.5,
            outline: "none",
            opacity: busy ? 0.6 : 1,
          }}
        />
        <button
          type="button"
          className="chat-send"
          onClick={submit}
          disabled={busy || draft.trim() === ""}
          aria-label="Send message"
          style={{
            // 38px tall but padded wide to clear the 44px touch target.
            height: 38,
            minWidth: 44,
            padding: "0 14px",
            background: "var(--accent)",
            border: "1px solid var(--accent)",
            borderRadius: "var(--r-2)",
            color: "var(--accent-fg)",
            fontFamily: "var(--font-sans)",
            fontSize: 13,
            fontWeight: 600,
            cursor: busy || draft.trim() === "" ? "default" : "pointer",
            opacity: busy || draft.trim() === "" ? 0.45 : 1,
            flexShrink: 0,
          }}
        >
          Send
        </button>
      </div>
    </div>
  );
}
