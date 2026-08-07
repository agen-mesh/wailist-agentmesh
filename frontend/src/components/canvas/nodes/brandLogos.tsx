import {
  siAnthropic,
  siGooglegemini,
  siMistralai,
  siDiscord,
  siGooglechat,
  siNtfy,
  siTelegram,
  siGithub,
  siGitlab,
  siJira,
  siLinear,
  siSentry,
  siNotion,
  siAirtable,
  siTrello,
  siAsana,
  siClickup,
  siTodoist,
  siHubspot,
  siMailchimp,
  siSupabase,
  siWoocommerce,
  siElevenlabs,
  type SimpleIcon,
} from "simple-icons";
import type { ReactNode } from "react";

// Maps a node template id to its simple-icons export. The logo is drawn in
// currentColor (monochrome) so it inherits the exact colour the placeholder
// letter used -- the node's icon box, size, and styling are untouched; only the
// glyph shape changes from a letter to the service's real mark.
//
// Imports are static (not a dynamic simpleIcons[name] lookup) so the bundler
// can tree-shake everything but these ~23 icons instead of shipping all
// ~3450 in simple-icons. OpenAI and Groq are deliberately absent: simple-icons
// no longer ships either mark (removed at the brands' request, like Slack and
// Microsoft Teams below), and unlike Slack/Teams no custom mark has been drawn
// for them yet, so those two templates fall back to the placeholder letter.
const BRAND_ICONS: Record<string, SimpleIcon> = {
  // LLM providers
  anthropic: siAnthropic,
  gemini: siGooglegemini,
  mistral: siMistralai,
  // Messaging
  discord: siDiscord,
  google_chat: siGooglechat,
  ntfy: siNtfy,
  telegram: siTelegram,
  // Dev tools
  github: siGithub,
  gitlab: siGitlab,
  jira: siJira,
  linear: siLinear,
  sentry: siSentry,
  // Productivity
  notion: siNotion,
  airtable: siAirtable,
  trello: siTrello,
  asana: siAsana,
  clickup: siClickup,
  todoist: siTodoist,
  // Data / commerce
  hubspot: siHubspot,
  mailchimp: siMailchimp,
  supabase: siSupabase,
  woocommerce: siWoocommerce,
  // Media
  elevenlabs: siElevenlabs,
};

function iconFor(template?: string): SimpleIcon | undefined {
  if (!template) return undefined;
  return BRAND_ICONS[template];
}

// Custom marks for brands simple-icons no longer ships (Slack and Microsoft
// Teams were both removed at the brands' request). Drawn in currentColor on the
// same 24-unit grid so they match every other logo and the node's icon box.
const CUSTOM_LOGOS: Record<
  string,
  { title: string; render: (size: number) => ReactNode }
> = {
  slack: {
    title: "Slack",
    render: (size) => (
      <svg
        role="img"
        aria-label="Slack"
        viewBox="0 0 24 24"
        width={size}
        height={size}
        fill="currentColor"
        style={{ display: "block" }}
      >
        {[0, 90, 180, 270].map((a) => (
          <g key={a} transform={`rotate(${a} 12 12)`}>
            <rect x="12" y="9.3" width="6.4" height="2.7" rx="1.35" />
            <rect x="15.7" y="12" width="2.7" height="2.7" rx="1.35" />
          </g>
        ))}
      </svg>
    ),
  },
  teams: {
    title: "Microsoft Teams",
    render: (size) => (
      <svg
        role="img"
        aria-label="Microsoft Teams"
        viewBox="0 0 24 24"
        width={size}
        height={size}
        fill="currentColor"
        style={{ display: "block" }}
      >
        <circle cx="8" cy="5.6" r="2.3" />
        <path
          fillRule="evenodd"
          clipRule="evenodd"
          d="M10 7 h8 a2 2 0 0 1 2 2 v8 a2 2 0 0 1 -2 2 h-8 a2 2 0 0 1 -2 -2 v-8 a2 2 0 0 1 2 -2 Z M11 10 v1.7 h2.1 v5.3 h1.8 v-5.3 h2.1 v-1.7 Z"
        />
      </svg>
    ),
  },
};

// Renders the service's real brand mark in place of the placeholder letter when
// one exists for the node's template; otherwise renders the letter unchanged.
export function BrandLogo({
  template,
  fallback,
  size = 15,
}: {
  template?: string;
  fallback: ReactNode;
  size?: number;
}) {
  const custom = template ? CUSTOM_LOGOS[template] : undefined;
  if (custom) return <>{custom.render(size)}</>;

  const icon = iconFor(template);
  if (!icon) return <>{fallback}</>;
  return (
    <svg
      role="img"
      aria-label={icon.title}
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="currentColor"
      style={{ display: "block" }}
    >
      <path d={icon.path} />
    </svg>
  );
}
