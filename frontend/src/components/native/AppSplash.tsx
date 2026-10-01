"use client";
import { useEffect, useState } from "react";
import { IS_NATIVE, authReady } from "@/lib/nativeAuth";

// The app's own loading screen: the wordmark on black, its colours sweeping
// across, held while the shell actually boots.
//
// Why this exists in the WebView rather than natively. From Android 12 the
// system draws the launch window itself and it cannot be fully customised --
// an icon on a colour, animation capped at 1000ms, and no way to put a wordmark
// there at all. values/styles.xml makes that first frame black with the mark on
// it; this picks up immediately after, and the two are the same colour so the
// join is invisible.
//
// It is also what closes the white flash. Android removes a launch window "as
// soon as the first frame is drawn" -- and the first frame is the empty WebView,
// not the mounted app. With launchAutoHide off in capacitor.config.ts, the
// native splash stays up until this component has painted and calls hide(), so
// there is never a moment with nothing on screen.

// How long the wordmark is held at minimum, and this one is a product decision
// rather than a technical floor.
//
// 700ms was the technical answer: one full sweep, so a fast boot could not flash
// the wordmark and vanish, which reads as a glitch rather than a load. 1700 is
// the deliberate one. Seen on real hardware the screen is worth looking at, and
// a launch that is over before it registers gives that away for nothing.
//
// It also buys the shell a second. NativeBoot's work runs underneath this, so a
// longer hold means more of it is finished before anything is revealed -- fewer
// screens that arrive empty and populate a moment later. That is a side effect
// of holding the screen, not a wait ON the backend: nothing here blocks on a
// request, and a slow server still resolves through authReady exactly as before.
//
// Android's guidance is against artificial timers, and this is one, so it is
// worth being honest about the trade: one second of brand against one second of
// waiting. It is defensible while the screen carries something worth seeing. If
// the wordmark ever goes, this number should go back to 700 with it.
//
// Measured from when the wordmark is VISIBLE, not from mount. Those are not the
// same moment: the native splash sits in its own window on top of the WebView
// until hide() completes, so a floor started at mount can elapse entirely
// behind it.
const MIN_VISIBLE_MS = 1700;

// The backstop, and nothing else. It must be LONGER than the boot it is
// covering or it becomes the normal way out -- which is what went wrong: at
// 2000ms against NativeBoot's 10s timeout, any boot slower than two seconds hit
// the cap, and the cap tore the wordmark away while the app behind it had
// still drawn nothing. What the user got was the splash replaced by an empty
// screen for as long as the app took to render.
//
// authReady always settles inside BOOT_TIMEOUT_MS because NativeBoot resolves
// it in a finally, so this only fires if that contract is broken. Sitting on a
// branded screen for a moment longer is strictly better than sitting on a blank
// one: Android's guidance is that a splash lasts as long as loading takes, and
// loading is not over while the screen is still empty.
const MAX_VISIBLE_MS = 12_000;

// Matches the fade in globals.css. Kept as a constant because the unmount has
// to outlast the transition, and two numbers drifting apart would clip it.
const FADE_MS = 220;

// How long the native splash is allowed to wait for the wordmark's real font,
// and why waiting at all is necessary.
//
// next/font builds its metric-corrected fallback face out of `local(Arial)`:
//
//   @font-face{font-family:geistSans Fallback;src:local(Arial);
//              ascent-override:94.56%;size-adjust:106.28%}
//
// Android has no Arial. That face never resolves, so the WebView falls all the
// way through to Roboto with NONE of those overrides applied, and Geist then
// arrives a frame or two later under font-display:swap. Measured on the shipped
// bundle at this component's own 600/34px: Geist sets "AgentMesh" at 179.97px,
// Roboto at 160.09px. The mark is centred, so the swap moved each end of the
// wordmark about ten CSS pixels sideways -- a visible flinch, once, per launch.
//
// It never showed on the web because desktops HAVE Arial: there the fallback
// resolves and the same measurement is 176.67px, three pixels off rather than
// twenty. This is an Android-only defect by construction.
//
// The fix is ordering, not metrics. The native splash is already held open by
// launchAutoHide:false, so the fallback frame can simply be painted BEHIND it:
// wait for fonts before hide(), and the first frame anyone sees is already set
// in Geist. Hand-tuning a size-adjust for Roboto instead would be a guess that
// has to be re-guessed for every device font.
//
// Capped, because a font that never arrives must not strand boot behind the
// native splash. The woff2 is on the device and <link rel=preload>ed, so in
// practice this settles in tens of milliseconds and the cap never fires.
const FONT_WAIT_MS = 1000;

// Is there anything behind the splash to reveal?
//
// authReady says the SHELL has booted. It does not say React has rendered a
// screen, and on a slow device those are seconds apart -- long enough that
// leaving on authReady alone uncovered an empty page and held it there.
//
// "Occupies space" was the first version of this test and it never waited for
// anything. The static export ships a full-height placeholder as the second
// child of <body>,
//
//   <div><div style="min-height:100dvh;background:var(--bg)"></div></div>
//
// which is exactly the empty screen this is supposed to hold past, and it has
// height on the very first frame. app/page.tsx renders that same placeholder
// while it decides between /signin and /workflows, so it is the normal state
// during boot rather than an edge case: the gate returned true immediately,
// every launch, and the guard behind it was decorative.
//
// So the test is CONTENT, not size: rendered text, or something that is content
// without being text. Still deliberately generic -- it never has to know which
// route the app opened on or what that route calls its root element.
//
// textContent rather than innerText: jsdom does not implement innerText, and
// the difference (visibility-aware whitespace) buys nothing here.
const CONTENTFUL = "img, svg, canvas, input, button, textarea, select";

export function appHasPainted(body: HTMLElement = document.body): boolean {
  return Array.from(body.children).some((el) => {
    if (!(el instanceof HTMLElement)) return false;
    if (el.classList.contains("splash")) return false;
    if (el.getBoundingClientRect().height <= 0) return false;
    return (
      (el.textContent ?? "").trim() !== "" ||
      el.querySelector(CONTENTFUL) !== null
    );
  });
}

export function AppSplash() {
  // Starts visible, and deliberately not behind a mounted check: the shell is a
  // static export, so this renders into index.html and is on screen in the very
  // first frame the WebView paints. Anything gated on an effect would show the
  // app first and the splash after, which is worse than no splash.
  const [phase, setPhase] = useState<"visible" | "leaving" | "gone">("visible");

  useEffect(() => {
    if (!IS_NATIVE) return;

    let done = false;
    let capTimer: number | undefined;
    let floorTimer: number | undefined;
    let fontTimer: number | undefined;
    let frame: number | undefined;

    const leave = () => {
      if (done) return;
      done = true;
      setPhase("leaving");
      window.setTimeout(() => setPhase("gone"), FADE_MS);
    };

    // Poll on frames rather than a timer: this is a question about what has been
    // drawn, so the moment after a paint is exactly when the answer changes. The
    // splash is animating throughout, so frames are being served -- rAF stalling
    // on an unpainted page is not a risk while this component is the page.
    const leaveOncePainted = () => {
      if (done) return;
      if (appHasPainted()) return leave();
      frame = requestAnimationFrame(leaveOncePainted);
    };

    // The clock starts when the wordmark can actually be SEEN.
    //
    // hide() takes the native splash down, and until it resolves this component
    // is painted but covered. Starting the floor and the cap here rather than at
    // mount is the whole fix: previously both ran while the native window was
    // still on top, so the wordmark's entire life could expire before anyone
    // could see it -- which is what happened, every launch.
    const begin = () => {
      if (done) return;
      const shownAt = Date.now();
      capTimer = window.setTimeout(leave, MAX_VISIBLE_MS);
      void authReady.then(() => {
        const remaining = MIN_VISIBLE_MS - (Date.now() - shownAt);
        if (remaining > 0)
          floorTimer = window.setTimeout(leaveOncePainted, remaining);
        else leaveOncePainted();
      });
    };

    // Wait for the wordmark's real font, then one frame for the re-layout to
    // paint. Both happen while the native splash is still on top, so the
    // fallback setting of the wordmark never reaches the screen. See
    // FONT_WAIT_MS for the measurements behind this.
    //
    // document.fonts.ready and not fonts.load(): the family name next/font
    // generates is a build hash, so naming it here would be a string that
    // silently stops matching one build later. This component is in the first
    // frame of the document, so its font request is already pending by the time
    // this effect runs and ready cannot resolve ahead of it.
    const fontsSettled = () =>
      Promise.race([
        document.fonts
          ? document.fonts.ready.then(
              () => new Promise<void>((r) => requestAnimationFrame(() => r())),
            )
          : Promise.resolve(),
        new Promise<void>((r) => {
          fontTimer = window.setTimeout(r, FONT_WAIT_MS);
        }),
      ]);

    // Tell the OS it can drop the launch window: this component is painted, so
    // there is something behind it. The result is not awaited for correctness --
    // a missing plugin must not stall boot -- but begin() hangs off it either
    // way, so a rejection still starts the clock rather than stranding it.
    void fontsSettled()
      .then(() => import("@capacitor/splash-screen"))
      .then(({ SplashScreen }) => SplashScreen.hide())
      .catch(() => {})
      .finally(begin);

    return () => {
      done = true;
      window.clearTimeout(capTimer);
      window.clearTimeout(floorTimer);
      window.clearTimeout(fontTimer);
      if (frame !== undefined) cancelAnimationFrame(frame);
    };
  }, []);

  // Web builds never render this: IS_NATIVE is false, so it returns null before
  // any markup exists and the browser's prerendered HTML contains no splash.
  //
  // The JSX below does still ship in the client chunk -- checked, rather than
  // assumed, because the same claim about the landing page in #166 was true and
  // it is tempting to reuse it. It is not true here: nothing removes an
  // unreached return, so a few hundred bytes of inert markup ride along. That
  // is the honest cost, and it is small enough to accept rather than paying for
  // a dynamic import on the one component that must be in the first frame.
  if (!IS_NATIVE || phase === "gone") return null;

  return (
    <div
      className="splash"
      data-leaving={phase === "leaving" ? "" : undefined}
      // Not a live region and not a progress bar: this is the app opening, and
      // a screen reader announcing "loading" over a 700ms logo is noise. It is
      // hidden from the tree entirely, and the screen behind it is what gets
      // announced once it goes.
      aria-hidden="true"
    >
      <span className="splash__mark">
        Agent<span className="splash__sweep">Mesh</span>
      </span>
    </div>
  );
}
