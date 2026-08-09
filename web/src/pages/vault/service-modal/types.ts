import type { Auth, Substitution } from "../../../components/ProposalPreview";

/** Service as stored in the vault broker config (read/write surface). */
export interface Service {
  name: string;
  host: string;
  enabled?: boolean;
  auth: Auth;
  substitutions?: Substitution[];
}

/** Catalog template as served by /v1/service-catalog. */
export interface CatalogTemplate {
  id: string;
  name: string;
  host: string;
  description: string;
  category?: string;
  aliases?: string[];
  auth_type: string;
  suggested_credential_key: string;
  header?: string;
  prefix?: string;
  headers?: Record<string, string>;
  substitutions?: Substitution[];
  placeholder_prefix?: string;
  placeholder_length?: number;
}

export type AuthType = "bearer" | "basic" | "api-key" | "custom" | "passthrough";

export const AUTH_TYPE_OPTIONS: { value: AuthType; label: string }[] = [
  { value: "passthrough", label: "Passthrough" },
  { value: "bearer", label: "Bearer" },
  { value: "basic", label: "Basic" },
  { value: "api-key", label: "API key" },
  { value: "custom", label: "Custom" },
];

/** Short badge labels for template cards (auth_type → display). */
export const AUTH_TYPE_BADGES: Record<string, string> = {
  passthrough: "Passthrough",
  bearer: "Bearer",
  basic: "Basic",
  "api-key": "API key",
  custom: "Headers",
};

// Display order mirrors catalog.Categories in internal/catalog/catalog.go.
export const CATEGORY_ORDER = [
  "AI & LLM",
  "Developer Tools",
  "Communication",
  "Cloud & Infra",
  "Business",
] as const;

/** Custom-header editor row. _id is local-only — stripped before submit. */
export interface HeaderRow {
  _id: number;
  name: string;
  value: string;
}

/** Substitution editor row. _id is local-only — stripped before submit. */
export interface SubRow {
  _id: number;
  key: string;
  placeholder: string;
  in: string[];
  env: string;
}

/** Flat, fully-editable representation of the service form. */
export interface ServiceFormState {
  name: string;
  host: string;
  enabled: boolean;
  authType: AuthType;
  /** bearer */
  token: string;
  /** basic */
  username: string;
  password: string;
  /** api-key */
  apiKey: string;
  apiKeyHeader: string;
  apiKeyPrefix: string;
  /** custom */
  customHeaders: HeaderRow[];
  /** substitutions — composes with every auth type */
  subs: SubRow[];
}
