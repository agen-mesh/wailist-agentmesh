import type { Metadata } from "next";
import Link from "next/link";

// The privacy policy.
//
// Written because Google Play requires a reachable privacy-policy URL the
// moment an app declares ACCESS_BACKGROUND_LOCATION, which the Android app
// does (#134). It is also the document Play's Data Safety form is checked
// against, so the location section below is deliberately specific: it states
// what the server actually keeps -- a derived enter/leave boolean and a
// timestamp, never coordinates -- because that is what migration 000029 stores
// and what the in-app disclosure already promises.
//
// The presentational helpers here are local rather than shared with /terms.
// Extracting them would mean editing a live legal page to no functional end;
// if a third legal page ever appears, that is the moment to factor them out.

export const metadata: Metadata = {
  title: "Privacy Policy | AgentMesh",
  description:
    "How AgentMesh handles your data, including location used for workflow triggers.",
};

const EFFECTIVE = "3 September 2026";
const COMPANY = "AgentMesh";
const CONTACT = "privacy@agent-mesh.app";

export default function PrivacyPage() {
  return (
    <div
      style={{
        background: "var(--bg)",
        minHeight: "100dvh",
        color: "var(--fg)",
      }}
    >
      <div
        style={{
          borderBottom: "1px solid var(--border)",
          padding: "16px 32px",
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
        }}
      >
        <Link
          href="/"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 14,
            fontWeight: 700,
            color: "var(--fg)",
            textDecoration: "none",
            letterSpacing: "-0.01em",
          }}
        >
          ← AgentMesh
        </Link>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            color: "var(--fg-dim)",
          }}
        >
          Effective {EFFECTIVE}
        </span>
      </div>

      <main
        style={{ maxWidth: 740, margin: "0 auto", padding: "64px 32px 96px" }}
      >
        <div style={{ marginBottom: 56 }}>
          <div style={badge}>Legal</div>
          <h1
            style={{
              margin: 0,
              fontSize: 40,
              fontWeight: 600,
              letterSpacing: "-0.025em",
              lineHeight: 1.15,
            }}
          >
            Privacy Policy
          </h1>
          <p
            style={{
              margin: "12px 0 0",
              color: "var(--fg-muted)",
              fontSize: 15,
              lineHeight: 1.6,
            }}
          >
            What {COMPANY} collects, what it does not, and what you can do about
            it.
          </p>
        </div>

        <div style={{ display: "flex", flexDirection: "column" }}>
          <Section n="1" title="What this covers">
            <P>
              This policy covers the {COMPANY} web application at agent-mesh.app
              and the {COMPANY} Android app. Where the two differ (and on
              location, they differ considerably) the difference is stated.
            </P>
          </Section>

          <Section n="2" title="Information you give us">
            <P>
              <strong>Account details.</strong> Your email address and a hashed
              password. We never store your password itself.
            </P>
            <P>
              <strong>Workflow content.</strong> The workflows you build, the
              configuration you enter into them, and the record of the runs they
              produce. Credentials you enter for third-party connectors are
              encrypted before storage.
            </P>
            <P>
              <strong>Billing information.</strong> Records of credits purchased
              and consumed, and of on-chain payments made by your workflows.
            </P>
          </Section>

          <Section n="3" title="Location, in the Android app">
            <P>
              This section exists because it is the part people most want a
              straight answer about.
            </P>
            <P>
              The Android app can start a workflow when you cross the edge of a
              place you have chosen. To do that, Android must be allowed to
              check your location while the app is closed. This is entirely
              optional: everything else in the app works without it, and the
              feature stays off until you turn it on.
            </P>
            <Notice>
              <strong>We do not keep a record of where you have been.</strong>{" "}
              When your device reports a position, it is used to answer one
              question, whether you are inside the zone you chose or outside it,
              and then discarded. What our servers retain is that answer and the
              time it was given. There is no history of coordinates for us to
              search, hand over, or lose.
            </Notice>
            <P>
              <strong>On your device.</strong> A crossing that happens with no
              signal is held on the phone until it can be sent, so it is not
              lost. Those pending readings do include your position. They never
              leave the device except to report that one crossing, and each is
              deleted as soon as it is sent, or within a day if it never can be.
            </P>
            <P>
              <strong>Turning it off.</strong> Remove the zone in the app, or
              revoke location permission in Android settings. Removing the zone
              also clears the state described above.
            </P>
          </Section>

          <Section n="4" title="Notifications">
            <P>
              If you allow notifications, your device is issued a registration
              token by Google&rsquo;s Firebase Cloud Messaging, and we store
              that token so we can tell your device when one of your workflows
              finishes. It identifies the app installation, not you. Signing out
              removes it, and a token that stops working is deleted.
            </P>
          </Section>

          <Section n="5" title="What we do not do">
            <P>
              We do not sell your personal information. We do not share it with
              third parties for their own advertising or marketing. We do not
              use your workflow content to train machine-learning models.
            </P>
          </Section>

          <Section n="6" title="Service providers">
            <P>
              Running the product means some data passes through others: hosting
              and database providers, Google&rsquo;s Firebase Cloud Messaging
              for notifications, payment and blockchain infrastructure for
              billing, and the AI model providers your workflows are configured
              to call. A workflow that calls an external model or tool sends
              that provider whatever the workflow gives it. You choose those
              connections, and their own privacy policies apply to them.
            </P>
          </Section>

          <Section n="7" title="Retention and deletion">
            <P>
              Account and workflow data is kept while your account is open.
              Deleting a workflow deletes its configuration and its run history.
              To delete your account and the data associated with it, write to{" "}
              <a href={`mailto:${CONTACT}`} style={inlineLink}>
                {CONTACT}
              </a>
              .
            </P>
          </Section>

          <Section n="8" title="Security">
            <P>
              Traffic is encrypted in transit. Connector credentials are
              encrypted at rest, and on Android the session token is held in
              storage encrypted with a key kept in the device&rsquo;s hardware
              keystore. No system is perfect, and we do not claim otherwise.
            </P>
          </Section>

          <Section n="9" title="Children">
            <P>
              {COMPANY} is not intended for children under 13, and we do not
              knowingly collect their information.
            </P>
          </Section>

          <Section n="10" title="Changes">
            <P>
              If this policy changes materially, we will update the effective
              date above and notify account holders. Continuing to use {COMPANY}{" "}
              after a change means you accept it.
            </P>
          </Section>

          <Section n="11" title="Contact">
            <P>
              Questions, requests, or complaints:{" "}
              <a href={`mailto:${CONTACT}`} style={inlineLink}>
                {CONTACT}
              </a>
              .
            </P>
          </Section>
        </div>

        <div
          style={{
            marginTop: 64,
            paddingTop: 32,
            borderTop: "1px solid var(--border)",
            display: "flex",
            gap: 24,
            fontFamily: "var(--font-mono)",
            fontSize: 12,
          }}
        >
          <Link href="/terms" style={inlineLink}>
            Terms &amp; Conditions
          </Link>
          <Link href="/refund-policy" style={inlineLink}>
            Cancellation &amp; Refund Policy
          </Link>
        </div>
      </main>
    </div>
  );
}

const badge: React.CSSProperties = {
  display: "inline-block",
  fontFamily: "var(--font-mono)",
  fontSize: 11,
  letterSpacing: "0.1em",
  textTransform: "uppercase",
  color: "var(--accent)",
  background: "var(--accent-soft)",
  border: "1px solid var(--accent-line)",
  borderRadius: 999,
  padding: "3px 12px",
  marginBottom: 20,
};

const inlineLink: React.CSSProperties = {
  color: "var(--accent)",
  textDecoration: "underline",
};

function Section({
  n,
  title,
  children,
}: {
  n: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section style={{ marginBottom: 40 }}>
      <h2
        style={{
          margin: "0 0 12px",
          fontSize: 18,
          fontWeight: 600,
          letterSpacing: "-0.01em",
          display: "flex",
          gap: 12,
          alignItems: "baseline",
        }}
      >
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 12,
            color: "var(--fg-dim)",
          }}
        >
          {n}
        </span>
        {title}
      </h2>
      {children}
    </section>
  );
}

function P({ children }: { children: React.ReactNode }) {
  return (
    <p
      style={{
        margin: "0 0 12px",
        color: "var(--fg-muted)",
        fontSize: 14,
        lineHeight: 1.7,
        maxWidth: "68ch",
      }}
    >
      {children}
    </p>
  );
}

function Notice({ children }: { children: React.ReactNode }) {
  return (
    <div
      style={{
        border: "1px solid var(--accent-line)",
        background: "var(--accent-soft)",
        borderRadius: "var(--r-2)",
        padding: "14px 16px",
        margin: "0 0 12px",
        color: "var(--fg)",
        fontSize: 14,
        lineHeight: 1.7,
        maxWidth: "68ch",
      }}
    >
      {children}
    </div>
  );
}
