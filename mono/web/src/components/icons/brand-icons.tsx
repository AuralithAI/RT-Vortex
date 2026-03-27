// ─── Brand Icons ─────────────────────────────────────────────────────────────
// Production-grade SVG brand icons for OAuth providers, VCS platforms, and LLM
// providers. Inline SVGs for zero external dependencies and instant rendering.
// ─────────────────────────────────────────────────────────────────────────────

import type { SVGProps } from "react";

type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function defaults(props: IconProps, fallbackSize = 24) {
  const { size, width, height, ...rest } = props;
  return {
    width: width ?? size ?? fallbackSize,
    height: height ?? size ?? fallbackSize,
    ...rest,
  };
}

// ── OAuth / SSO Providers ───────────────────────────────────────────────────

export function GoogleIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92a5.06 5.06 0 0 1-2.2 3.32v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.1Z" fill="#4285F4" />
      <path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23Z" fill="#34A853" />
      <path d="M5.84 14.09A6.97 6.97 0 0 1 5.48 12c0-.72.13-1.43.36-2.09V7.07H2.18A11.96 11.96 0 0 0 1 12c0 1.94.46 3.77 1.18 5.09l3.66-2.84v-.16Z" fill="#FBBC05" />
      <path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53Z" fill="#EA4335" />
    </svg>
  );
}

export function MicrosoftIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <rect x="1" y="1" width="10" height="10" fill="#F25022" />
      <rect x="13" y="1" width="10" height="10" fill="#7FBA00" />
      <rect x="1" y="13" width="10" height="10" fill="#00A4EF" />
      <rect x="13" y="13" width="10" height="10" fill="#FFB900" />
    </svg>
  );
}

export function AppleIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="M18.71 19.5c-.83 1.24-1.71 2.45-3.05 2.47-1.34.03-1.77-.79-3.29-.79-1.53 0-2 .77-3.27.82-1.31.05-2.3-1.32-3.14-2.53C4.25 17 2.94 12.45 4.7 9.39c.87-1.52 2.43-2.48 4.12-2.51 1.28-.02 2.5.87 3.29.87.78 0 2.26-1.07 3.8-.91.65.03 2.47.26 3.64 1.98-.09.06-2.17 1.28-2.15 3.81.03 3.02 2.65 4.03 2.68 4.04-.03.07-.42 1.44-1.38 2.83ZM13 3.5c.73-.83 1.94-1.46 2.94-1.5.13 1.17-.34 2.35-1.04 3.19-.69.85-1.83 1.51-2.95 1.42-.15-1.15.41-2.35 1.05-3.11Z" />
    </svg>
  );
}

// ── VCS Platforms ───────────────────────────────────────────────────────────

export function GitHubIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.832.092-.647.35-1.088.636-1.338-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0 1 12 6.836c.85.004 1.705.115 2.504.337 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10Z" />
    </svg>
  );
}

export function GitLabIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <path d="m12 22.178-4.233-13.03H16.233L12 22.178Z" fill="#E24329" />
      <path d="m12 22.178-4.233-13.03H1.87L12 22.178Z" fill="#FC6D26" />
      <path d="M1.87 9.148.085 14.64a1.216 1.216 0 0 0 .442 1.36L12 22.178 1.87 9.148Z" fill="#FCA326" />
      <path d="M1.87 9.148h5.898L5.285 1.73a.607.607 0 0 0-1.156 0L1.87 9.148Z" fill="#E24329" />
      <path d="m12 22.178 4.233-13.03h5.898L12 22.178Z" fill="#FC6D26" />
      <path d="m22.13 9.148 1.785 5.493a1.216 1.216 0 0 1-.442 1.36L12 22.178l10.13-13.03Z" fill="#FCA326" />
      <path d="M22.13 9.148h-5.898l2.483-7.418a.607.607 0 0 1 1.156 0l2.259 7.418Z" fill="#E24329" />
    </svg>
  );
}

export function BitbucketIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <path d="M2.65 3C2.3 3 2 3.3 2 3.68l.02.12 2.72 16.47c.07.42.43.73.85.73h13.05c.31 0 .58-.23.63-.54L22 3.8l-.02-.12A.67.67 0 0 0 21.35 3H2.65Zm11.58 13.12H9.84l-1.18-6.2h6.82l-1.25 6.2Z" fill="#2684FF" />
      <path d="m21.66 9.92h-6.18l-1.25 6.2H9.84l-5.78 6.85c.14.12.31.2.5.2h13.05c.31 0 .58-.23.63-.54l2.42-12.71Z" fill="url(#bb-grad)" />
      <defs>
        <linearGradient id="bb-grad" x1="22.55" y1="11.38" x2="11.42" y2="20.76" gradientUnits="userSpaceOnUse">
          <stop offset="0.18" stopColor="#0052CC" />
          <stop offset="1" stopColor="#2684FF" />
        </linearGradient>
      </defs>
    </svg>
  );
}

export function AzureDevOpsIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <path d="M22 5v14l-6 3V7.5L8.5 22H2l14-18.5v-1L7 6.5V2l15 3Z" fill="#0078D7" />
    </svg>
  );
}

// ── LLM Providers ───────────────────────────────────────────────────────────

export function OpenAIIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="M22.282 9.821a5.985 5.985 0 0 0-.516-4.91 6.046 6.046 0 0 0-6.51-2.9A6.065 6.065 0 0 0 4.981 4.18a5.985 5.985 0 0 0-3.998 2.9 6.046 6.046 0 0 0 .743 7.097 5.98 5.98 0 0 0 .51 4.911 6.051 6.051 0 0 0 6.515 2.9A5.985 5.985 0 0 0 13.26 24a6.056 6.056 0 0 0 5.772-4.206 5.99 5.99 0 0 0 3.997-2.9 6.056 6.056 0 0 0-.747-7.073ZM13.26 22.43a4.476 4.476 0 0 1-2.876-1.04l.141-.081 4.779-2.758a.795.795 0 0 0 .392-.681v-6.737l2.02 1.168a.071.071 0 0 1 .038.052v5.583a4.504 4.504 0 0 1-4.494 4.494ZM3.6 18.304a4.47 4.47 0 0 1-.535-3.014l.142.085 4.783 2.759a.771.771 0 0 0 .78 0l5.843-3.369v2.332a.08.08 0 0 1-.033.062L9.74 19.95a4.5 4.5 0 0 1-6.14-1.646ZM2.34 7.896a4.485 4.485 0 0 1 2.366-1.973V11.6a.766.766 0 0 0 .388.676l5.815 3.355-2.02 1.168a.076.076 0 0 1-.071 0l-4.83-2.786A4.504 4.504 0 0 1 2.34 7.872v.024Zm16.597 3.855-5.833-3.387L15.119 7.2a.076.076 0 0 1 .071 0l4.83 2.791a4.494 4.494 0 0 1-.676 8.105v-5.678a.79.79 0 0 0-.407-.667Zm2.01-3.023-.141-.085-4.774-2.782a.776.776 0 0 0-.785 0L9.409 9.23V6.897a.066.066 0 0 1 .028-.061l4.83-2.787a4.5 4.5 0 0 1 6.68 4.66v.018Zm-12.64 4.135-2.02-1.164a.08.08 0 0 1-.038-.057V6.075a4.5 4.5 0 0 1 7.375-3.453l-.142.08L8.704 5.46a.795.795 0 0 0-.393.681l-.004 6.722Zm1.097-2.365 2.602-1.5 2.607 1.5v2.999l-2.597 1.5-2.607-1.5-.005-2.999Z" />
    </svg>
  );
}

export function AnthropicIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="M13.827 3.52h3.603L24 20.48h-3.603l-6.57-16.96zm-7.257 0L0 20.48h3.603l1.326-3.63h6.57l1.326 3.63h3.603L9.858 3.52H6.57zm-.21 10.69 2.553-6.99 2.553 6.99H6.36z" />
    </svg>
  );
}

export function GeminiIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <path d="M12 24A14.3 14.3 0 0 0 0 12 14.3 14.3 0 0 0 12 0a14.3 14.3 0 0 0 0 24Z" fill="url(#gemini-grad)" />
      <defs>
        <linearGradient id="gemini-grad" x1="0" y1="0" x2="24" y2="24" gradientUnits="userSpaceOnUse">
          <stop stopColor="#1C7CEA" />
          <stop offset=".33" stopColor="#1C7CEA" />
          <stop offset=".67" stopColor="#A084EE" />
          <stop offset="1" stopColor="#F28B82" />
        </linearGradient>
      </defs>
    </svg>
  );
}

export function XAIIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="m3 2 7.5 10.476L3 22h1.71l6.574-8.357L16.5 22H22l-7.875-11L21 2h-1.71l-6.198 7.881L8 2H3Zm2.46 1.384h2.7l10.38 17.232h-2.7L5.46 3.384Z" />
    </svg>
  );
}

export function MetaIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="none" {...p}>
      <path d="M6.915 4.03c-1.968 0-3.402 1.303-4.377 3.216C1.56 9.16 1 11.508 1 13.988c0 1.675.373 3.05 1.112 3.932.71.846 1.734 1.29 2.903 1.29 1.021 0 1.868-.316 2.668-1.04.763-.69 1.499-1.727 2.317-3.186l1.068-1.893c.974-1.726 2.098-3.1 3.376-3.976C15.603 8.302 16.89 7.9 18.253 7.9c1.796 0 3.263.69 4.254 1.985C23.498 11.18 24 13.058 24 15.34c0 1.392-.208 2.607-.618 3.598-.39.95-.963 1.697-1.69 2.2l-1.18-.96c.577-.418 1.034-1.012 1.356-1.78.335-.793.5-1.777.5-2.93 0-1.97-.384-3.545-1.152-4.588-.737-1.002-1.812-1.52-3.196-1.52-1.073 0-2.05.377-2.897 1.134-.834.745-1.631 1.87-2.434 3.396l-1.013 1.93c-.89 1.595-1.747 2.77-2.672 3.504-.95.755-1.987 1.126-3.11 1.126-1.652 0-2.97-.665-3.82-1.93C1.17 17.59.69 15.874.69 13.86c0-2.654.588-5.106 1.79-7.142C3.88 4.672 5.348 3.54 7.05 3.54c.98 0 1.864.33 2.598.986.704.629 1.29 1.545 1.777 2.69l-1.263.614c-.388-.925-.83-1.636-1.34-2.113-.498-.466-1.113-.687-1.907-.687Z" fill="#0081FB" />
    </svg>
  );
}

export function OllamaIcon(props: IconProps) {
  const p = defaults(props);
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" {...p}>
      <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2Zm0 3c1.66 0 3 1.34 3 3v1c0 1.66-1.34 3-3 3S9 10.66 9 9V8c0-1.66 1.34-3 3-3Zm5.5 12.5c0 .28-.22.5-.5.5H7c-.28 0-.5-.22-.5-.5C6.5 14.57 9.07 12 12 12s5.5 2.57 5.5 5.5Z" />
    </svg>
  );
}

// ── Lookup helpers ──────────────────────────────────────────────────────────

/** Get the right icon component for an OAuth/SSO provider name. */
export function getAuthProviderIcon(name: string): React.ComponentType<IconProps> | null {
  const map: Record<string, React.ComponentType<IconProps>> = {
    github: GitHubIcon,
    gitlab: GitLabIcon,
    bitbucket: BitbucketIcon,
    google: GoogleIcon,
    microsoft: MicrosoftIcon,
    apple: AppleIcon,
    x: XAIIcon,
  };
  return map[name.toLowerCase()] ?? null;
}

/** Get the right icon component for a VCS platform name. */
export function getVCSIcon(name: string): React.ComponentType<IconProps> | null {
  const map: Record<string, React.ComponentType<IconProps>> = {
    github: GitHubIcon,
    gitlab: GitLabIcon,
    bitbucket: BitbucketIcon,
    azure_devops: AzureDevOpsIcon,
  };
  return map[name.toLowerCase()] ?? null;
}

/** Get the right icon component for an LLM provider name. */
export function getLLMIcon(name: string): React.ComponentType<IconProps> | null {
  const map: Record<string, React.ComponentType<IconProps>> = {
    openai: OpenAIIcon,
    anthropic: AnthropicIcon,
    gemini: GeminiIcon,
    google: GeminiIcon,
    grok: XAIIcon,
    xai: XAIIcon,
    ollama: OllamaIcon,
    meta: MetaIcon,
    llama: MetaIcon,
  };
  return map[name.toLowerCase()] ?? null;
}
