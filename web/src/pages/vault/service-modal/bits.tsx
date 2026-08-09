import type { ReactNode } from "react";
import { AUTH_TYPE_BADGES } from "./types";

/* -- Template icon: letter tile (no external images — self-hosted/offline safe) -- */

export function TemplateIcon({ name, className = "" }: { name: string; className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={`flex items-center justify-center rounded-md border border-border bg-surface-raised font-semibold text-text ${className}`}
    >
      {name.trim().charAt(0).toUpperCase() || "?"}
    </div>
  );
}

/* -- Auth type badge for template cards -- */

export function AuthTypeBadge({ authType, hasSubs }: { authType: string; hasSubs?: boolean }) {
  const label = AUTH_TYPE_BADGES[authType] ?? authType;
  return (
    <span className="text-[10px] font-mono uppercase tracking-[0.14em] text-text-dim">
      {label}
      {hasSubs ? " + subs" : ""}
    </span>
  );
}

/* -- Eyebrow section label -- */

export function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <p className="mb-3 text-[11px] font-mono uppercase tracking-[0.18em] text-text-muted">
      {children}
    </p>
  );
}

/* -- Small remove (X) button -- */

export function IconButton({ onClick, ariaLabel }: { onClick: () => void; ariaLabel: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={ariaLabel}
      className="w-8 h-8 flex-shrink-0 flex items-center justify-center rounded-lg text-text-dim hover:text-danger hover:bg-danger-bg transition-colors"
    >
      <svg
        className="w-4 h-4"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <line x1="18" y1="6" x2="6" y2="18" />
        <line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>
  );
}

/* -- Field error text -- */

export function FieldErrorText({ children }: { children?: ReactNode }) {
  if (!children) return null;
  return <p className="mt-1.5 text-xs text-danger">{children}</p>;
}
