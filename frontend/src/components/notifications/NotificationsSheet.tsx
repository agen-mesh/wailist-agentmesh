"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { IS_NATIVE } from "@/lib/nativeAuth";
import { useScrollLock } from "@/hooks/useScrollLock";
import { ghostBtn, primaryBtn } from "@/components/ui/buttons";

// Where notifications are turned on, and the only place they can be.
//
// The whole FCM path -- permission, registration, the server's send rule --
// shipped in #132 with nothing calling it, so no permission was ever requested
// and no device was ever registered. This is that missing caller.
//
// A sheet rather than a row in the account menu, because a menu row has room
// for a label and nothing else, and three of the four states here need more
// than a label: the disclosure has to be READ before the OS dialog appears, a
// refusal needs a route to Settings, and an unsupported device needs an
// explanation rather than a control that cannot work.
//
// Modelled on CheckoutModal: a scoped <style> block for the things inline
// styles cannot express (keyframes, :hover, :focus-visible, reduced motion),
// and inline styles for everything else, which is how the rest of the app is
// written.

// What is shown once Android has been asked and told no.
//
// It does not nag. A refusal is an answer, and re-prompting is how an app gets
// uninstalled. Android will not show the dialog a second time either way -- on
// 13+ it is one-shot -- so Settings is not one option among several, it is the
// only route back, and saying so plainly is more useful than a retry button
// that would silently do nothing.
const DENIED_COPY = {
  title: "Notifications are off",
  body:
    "Android is not letting AgentMesh notify you, and it will not ask again " +
    "from inside the app. Everything else works as normal; you just have to " +
    "open a run to see how it went.",
  action: "Open Settings",
} as const;

// No Firebase in this build, or a device with no Play services. Distinct from
// a refusal: nobody said no, so offering a route to Settings would send the
// reader somewhere that cannot help.
const UNAVAILABLE_COPY = {
  title: "Notifications are not available here",
  body:
    "This device or this build cannot receive them. Nothing is wrong with " +
    "your account, and workflows keep running either way.",
} as const;

const SHEET_CSS = `
.notif-scrim {
  position: fixed; inset: 0; z-index: 1000;
  background: rgba(8,7,12,0.72); backdrop-filter: blur(4px);
  display: flex; align-items: flex-end; justify-content: center;
  animation: notif-scrim-in 0.18s var(--ease);
}
.notif-panel {
  position: relative;
  width: 100%; max-width: min(520px, 100vw);
  max-height: 88dvh; overflow-y: auto;
  padding: 20px 20px calc(20px + var(--safe-bottom, 0px));
  border: 1px solid var(--border-strong);
  border-radius: var(--r-4) var(--r-4) 0 0;
  background: var(--bg-elev-1);
  color: var(--fg);
  box-shadow: 0 -12px 40px rgba(0,0,0,0.55);
  animation: notif-panel-in 0.22s var(--ease);
}
/* The 44px touch floor is NOT here. ghostBtn and primaryBtn set a 36px
   minHeight as an INLINE style, and an inline style beats any rule in a
   stylesheet -- a min-height of 44px written here computes to 36px and looks
   like it works. It is applied through touchTarget below instead, spread into
   the same style object it has to win against. Measured, not assumed: the
   first version of this file had the rule here and shipped 35px buttons.
   Everything below is a state a style attribute cannot express. */
.notif-action {
  transition: color 0.12s var(--ease), border-color 0.12s var(--ease),
    background 0.12s var(--ease), transform 0.12s var(--ease);
}
.notif-action:active { transform: scale(0.97); }
.notif-action:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
@keyframes notif-scrim-in { from { opacity: 0; } to { opacity: 1; } }
@keyframes notif-panel-in {
  from { opacity: 0; transform: translateY(12px) scale(0.985); }
  to { opacity: 1; transform: none; }
}
/* Above the phone width it is a centred dialog rather than a bottom sheet:
   a full-width bar pinned to the bottom of a desktop window reads as a
   cookie banner. */
@media (min-width: 640px) {
  .notif-scrim { align-items: center; padding: 24px; }
  .notif-panel { border-radius: var(--r-4); box-shadow: 0 24px 64px rgba(0,0,0,0.55); }
}
@media (prefers-reduced-motion: reduce) {
  .notif-scrim, .notif-panel { animation: none; }
  .notif-action { transition: none; }
  .notif-action:active { transform: none; }
}
`;

// "off" rather than "prompt": the same panel serves a device Android has never
// been asked about and one whose owner turned notifications off with the
// permission still granted. Both want the same thing said to them.
type View = "loading" | "off" | "granted" | "denied" | "unavailable";

export function NotificationsSheet({
  onClose,
  returnFocusTo,
}: {
  onClose: () => void;
  // Where focus goes when this closes. Passed in rather than remembered from
  // document.activeElement, because the control that opened this sheet is a
  // menu item that unmounts as the menu closes -- focusing a detached node
  // does nothing, and the browser drops focus to <body>, which loses a
  // keyboard user their place on the page entirely.
  returnFocusTo?: React.RefObject<HTMLElement | null>;
}) {
  const [view, setView] = useState<View>("loading");
  const [busy, setBusy] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  // Fallback for a caller that passed no ref: whatever had focus when this
  // mounted, used only if it is still in the document when we close.
  const openerRef = useRef<Element | null>(null);

  useScrollLock(true);

  // Read the current state WITHOUT asking. enableNotifications() answers by
  // requesting, and on Android 13+ that dialog is a one-shot -- using it to
  // find out what to draw would spend the single ask on rendering a switch.
  //
  // Returns the view rather than setting it, so the caller decides whether the
  // answer is still wanted. The mount effect's answer is not, if the sheet has
  // been closed by the time it arrives.
  const read = useCallback(async (): Promise<View> => {
    if (!IS_NATIVE) return "unavailable";
    try {
      const { shell } = await import("@/native");
      return await shell.notificationState();
    } catch {
      return "unavailable";
    }
  }, []);

  useEffect(() => {
    openerRef.current = document.activeElement;
    // Focus lands inside the panel rather than on a specific control: the
    // first thing here is the disclosure, and it is meant to be read before
    // anything is pressed.
    //
    // On the next task, not this one. The account menu closes in the same
    // update that opens this sheet, and the browser moves focus to <body> when
    // the focused menu item unmounts -- which happens AFTER this effect runs,
    // so focusing here and stopping would be silently undone. Measured: it
    // was.
    //
    // A timeout rather than requestAnimationFrame, which was tried first:
    // rAF does not run at all while the page is not being painted, so a sheet
    // opened in a backgrounded WebView would never receive focus. Where the
    // keyboard goes is correctness, not animation, and must not depend on
    // whether anything is being drawn.
    const frame = window.setTimeout(() => panelRef.current?.focus(), 0);

    let stale = false;
    void read().then((state) => {
      if (!stale) setView(state);
    });
    return () => {
      stale = true;
      clearTimeout(frame);
    };
  }, [read]);

  // Closes, and puts focus back where a keyboard user can carry on from.
  //
  // Deliberately here and not in an unmount cleanup, which was tried first.
  // Every route out of this sheet -- Escape, the scrim, each of its buttons --
  // goes through this one function, so the cleanup bought nothing; and in dev
  // React's StrictMode double-invokes effects, so that cleanup ran once while
  // the sheet was still open and pulled focus straight back out of it.
  // Measured, not reasoned about.
  const close = () => {
    onClose();
    // The caller's control first, then whatever had focus on mount, and only
    // if that is still attached -- isConnected is the whole point, since the
    // control that opened this is a menu item the closing menu unmounts.
    const opener = openerRef.current;
    const fallback =
      opener instanceof HTMLElement && opener.isConnected ? opener : null;
    (returnFocusTo?.current ?? fallback)?.focus();
  };
  // Read through a ref by the Escape listener, so that listener registers once
  // instead of on every render. Assigned in an effect rather than during
  // render, which React forbids -- a ref written while rendering can be read
  // by a concurrent render that was never committed.
  const closeRef = useRef(close);
  useEffect(() => {
    closeRef.current = close;
  });

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeRef.current();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const enable = async () => {
    setBusy(true);
    try {
      const { shell } = await import("@/native");
      // enableNotifications answers "granted" | "denied" | "unavailable" --
      // never "off" -- so this is a complete View without a fallback branch.
      setView(await shell.enableNotifications());
    } catch {
      setView("unavailable");
    } finally {
      setBusy(false);
    }
  };

  const disable = async () => {
    setBusy(true);
    try {
      const { shell } = await import("@/native");
      await shell.disableNotifications();
      // Re-read rather than assuming: the answer depends on the opt-in flag
      // AND the OS permission, and only the first of those just changed.
      setView(await read());
    } catch {
      setView("unavailable");
    } finally {
      setBusy(false);
    }
  };

  const openSettings = async () => {
    const { openSettings: open } = await import("@/native/permissions");
    await open().catch(() => {});
  };

  return (
    <>
      <style>{SHEET_CSS}</style>
      <div
        className="notif-scrim"
        onClick={(e) => {
          if (e.target === e.currentTarget) close();
        }}
      >
        <div
          className="notif-panel"
          role="dialog"
          aria-modal="true"
          aria-label="Notifications"
          tabIndex={-1}
          ref={panelRef}
        >
          {view === "loading" && <p style={bodyText}>Checking this device…</p>}

          {view === "off" && (
            <Prompt busy={busy} onGrant={enable} onDecline={close} />
          )}

          {view === "granted" && (
            <Panel
              title="Notifications are on"
              body={
                "You will hear about runs you did not start -- a workflow " +
                "triggered by arriving somewhere, or by a schedule -- and " +
                "about every failure, whatever started it."
              }
            >
              <button
                type="button"
                className="notif-action"
                style={{ ...ghostBtn, ...touchTarget }}
                disabled={busy}
                onClick={disable}
              >
                {busy ? "Turning off" : "Turn off"}
              </button>
              <button
                type="button"
                className="notif-action"
                style={{ ...ghostBtn, ...touchTarget }}
                onClick={close}
              >
                Done
              </button>
            </Panel>
          )}

          {view === "denied" && (
            <Panel title={DENIED_COPY.title} body={DENIED_COPY.body}>
              <button
                type="button"
                className="notif-action"
                style={{ ...primaryBtn, ...touchTarget }}
                onClick={openSettings}
              >
                {DENIED_COPY.action}
              </button>
              <button
                type="button"
                className="notif-action"
                style={{ ...ghostBtn, ...touchTarget }}
                onClick={close}
              >
                Close
              </button>
            </Panel>
          )}

          {view === "unavailable" && (
            <Panel title={UNAVAILABLE_COPY.title} body={UNAVAILABLE_COPY.body}>
              <button
                type="button"
                className="notif-action"
                style={{ ...ghostBtn, ...touchTarget }}
                onClick={close}
              >
                Close
              </button>
            </Panel>
          )}
        </div>
      </div>
    </>
  );
}

// The disclosure, shown before Android's own dialog and never after it. Asking
// cold is refused far more often, and the ask cannot be repeated.
function Prompt({
  busy,
  onGrant,
  onDecline,
}: {
  busy: boolean;
  onGrant: () => void;
  onDecline: () => void;
}) {
  // Imported lazily with the rest of the native module so a browser build does
  // not pull Capacitor in for a string.
  const [copy, setCopy] = useState<{
    title: string;
    body: string;
    grant: string;
    decline: string;
  } | null>(null);

  useEffect(() => {
    void import("@/native/push").then((m) => setCopy({ ...m.PUSH_DISCLOSURE }));
  }, []);

  if (!copy) return <p style={bodyText}>Checking this device…</p>;

  return (
    <Panel title={copy.title} body={copy.body}>
      <button
        type="button"
        className="notif-action"
        style={{ ...primaryBtn, ...touchTarget }}
        disabled={busy}
        onClick={onGrant}
      >
        {busy ? "Asking Android" : copy.grant}
      </button>
      <button
        type="button"
        className="notif-action"
        style={{ ...ghostBtn, ...touchTarget }}
        onClick={onDecline}
      >
        {copy.decline}
      </button>
    </Panel>
  );
}

function Panel({
  title,
  body: text,
  children,
}: {
  title: string;
  body: string;
  children: React.ReactNode;
}) {
  return (
    <>
      <h2 style={heading}>{title}</h2>
      {/* Split on the blank lines the copy is written with, rather than
          rendering one wall of text. PUSH_DISCLOSURE separates the offer from
          the caveat on purpose. */}
      {text.split("\n\n").map((para, i) => (
        <p key={i} style={bodyText}>
          {para}
        </p>
      ))}
      <div style={actions}>{children}</div>
    </>
  );
}

const heading: React.CSSProperties = {
  margin: "0 0 10px",
  font: "600 17px/1.3 var(--font-sans)",
  color: "var(--fg)",
};

// ~60ch, the readable measure. This copy is the part that has to actually be
// read, so it must not run the full width of a tablet.
const bodyText: React.CSSProperties = {
  margin: "0 0 10px",
  maxWidth: "60ch",
  font: "400 13px/1.65 var(--font-sans)",
  color: "var(--fg-muted)",
};

// The floor every control here has to clear, spread AFTER the button style so
// it overrides its 36px. 44px is the smallest target a thumb hits reliably;
// the same number .pw-reveal and .am-sheet-grip hold to.
const touchTarget: React.CSSProperties = { minHeight: 44 };

const actions: React.CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  gap: 10,
  marginTop: 16,
};
