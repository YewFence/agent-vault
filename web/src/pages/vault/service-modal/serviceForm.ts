import {
  DEFAULT_SUBSTITUTION_SURFACES,
  type Auth,
} from "../../../components/ProposalPreview";
import type {
  AuthType,
  CatalogTemplate,
  HeaderRow,
  Service,
  ServiceFormState,
  SubRow,
} from "./types";

/* -- Row IDs -- */

// Module-level counter so editable rows key by identity rather than array
// index — deleting a middle row must not bleed input values between rows.
let rowIdSeq = 0;
const nextRowId = () => ++rowIdSeq;

/* -- Validation patterns (mirror internal/broker) -- */

// broker.ValidateSlug: 3–64 chars, lowercase alnum + hyphens, no
// leading/trailing/consecutive hyphens.
const SLUG_RE = /^[a-z0-9]+(-[a-z0-9]+)*$/;
// broker.CredentialKeyPattern: UPPER_SNAKE_CASE.
const CREDENTIAL_KEY_RE = /^[A-Z][A-Z0-9_]*$/;
// broker.EnvVarPattern: any valid shell identifier.
const ENV_VAR_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;
// broker header name pattern for custom auth.
const HEADER_NAME_RE = /^[a-zA-Z0-9-]+$/;
// broker hostLabelPattern.
const HOST_LABEL_RE = /^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$/i;
const IPV4_RE = /^(?:\d{1,3}\.){3}\d{1,3}$/;
const IPV6_RE = /^[0-9a-f:]+$/i;

/* -- Errors -- */

export interface SubRowErrors {
  key?: string;
  placeholder?: string;
  env?: string;
  surfaces?: string;
}

export interface StepErrors {
  fields: Record<string, string>;
  headerRows: Record<number, string>;
  subRows: Record<number, SubRowErrors>;
}

export const emptyStepErrors = (): StepErrors => ({ fields: {}, headerRows: {}, subRows: {} });

export const hasStepErrors = (e: StepErrors): boolean =>
  Object.keys(e.fields).length > 0 ||
  Object.keys(e.headerRows).length > 0 ||
  Object.keys(e.subRows).length > 0;

/* -- Field validators -- */

export function validateSlug(name: string): string | null {
  if (!name) return "Name is required";
  if (name.length < 3) return "Must be at least 3 characters";
  if (name.length > 64) return "Must be at most 64 characters";
  if (!SLUG_RE.test(name)) return "Lowercase letters, digits, and single hyphens only";
  return null;
}

// Mirrors broker.ValidateHost + SplitInlineHost (inline path/port) +
// ValidatePath, so bad patterns fail inline instead of at save time. The
// internal-host blocklist stays server-side (it depends on dev mode).
export function validateHostPattern(raw: string): string | null {
  const input = raw.trim();
  if (!input) return "Host pattern is required";
  if (input.includes("://")) return "Must not include a scheme (e.g. https://)";
  if (/[@?#\s]/.test(input)) return "Contains an invalid character";

  const slash = input.indexOf("/");
  const hostPort = slash === -1 ? input : input.slice(0, slash);
  const path = slash === -1 ? "" : input.slice(slash);
  if (path) {
    if (path.length > 256) return "Path is too long (max 256 characters)";
    if (path.includes("**")) return 'Path must not contain "**"';
    if (/[?#[\]\\|<>"\s]/.test(path)) return "Path contains an invalid character";
  }

  let host = hostPort;
  // IPv6 may be written as [addr]:port in the inline form. A bare IPv6
  // literal has no unambiguous inline port and is kept intact.
  if (hostPort.startsWith("[")) {
    const close = hostPort.indexOf("]");
    if (close === -1) return "Invalid bracketed IPv6 address";
    host = hostPort.slice(1, close);
    const rest = hostPort.slice(close + 1);
    if (rest && !/^:\d+$/.test(rest)) return "Invalid IPv6 host/port";
    if (rest) {
      const port = Number(rest.slice(1));
      if (port < 1 || port > 65535) return `"${hostPort}" has an invalid port`;
    }
  } else if (IPV6_RE.test(hostPort) && (hostPort.match(/:/g) ?? []).length > 1) {
    host = hostPort;
  } else {
    const colon = hostPort.lastIndexOf(":");
    if (colon !== -1) {
    const portStr = hostPort.slice(colon + 1);
    host = hostPort.slice(0, colon);
    if (!/^\d+$/.test(portStr)) return `"${hostPort}" has an invalid port`;
    const port = Number(portStr);
    if (port < 1 || port > 65535) return `"${hostPort}" has an invalid port`;
    }
  }

  if (!host) return "Host is required";
  const validIPv4 = IPV4_RE.test(host) && host.split(".").every((part) => Number(part) <= 255);
  const validIPv6 = IPV6_RE.test(host) && host.includes(":") && host.split(":").length >= 3;
  if (validIPv4 || validIPv6) return null;
  if (host.startsWith("*")) {
    if (host === "*") return "Bare wildcard is not allowed";
    if (!host.startsWith("*.")) return "Wildcard must be in the form *.example.com";
    const suffix = host.slice(2);
    if (!suffix.includes(".")) return "Wildcard must have at least two domain levels (e.g. *.example.com)";
    if (!HOST_LABEL_RE.test(suffix)) return "Invalid hostname in wildcard pattern";
    return null;
  }
  if (!HOST_LABEL_RE.test(host)) return "Not a valid hostname (e.g. api.stripe.com)";
  return null;
}

const checkCredentialKey = (value: string, label: string, required: boolean): string | null => {
  const v = value.trim();
  if (!v) return required ? `${label} is required` : null;
  if (!CREDENTIAL_KEY_RE.test(v)) return "Must be UPPER_SNAKE_CASE (e.g. STRIPE_KEY)";
  return null;
};

export function validateDetails(form: ServiceFormState, existingNames: string[]): StepErrors {
  const errors = emptyStepErrors();
  const name = form.name.trim();

  const slugError = validateSlug(name);
  if (slugError) {
    errors.fields.name = slugError;
  } else if (existingNames.includes(name)) {
    errors.fields.name = "A service with this name already exists in this vault.";
  }

  const hostError = validateHostPattern(form.host);
  if (hostError) errors.fields.host = hostError;

  return errors;
}

export function validateAuth(form: ServiceFormState): StepErrors {
  const errors = emptyStepErrors();
  switch (form.authType) {
    case "bearer": {
      const e = checkCredentialKey(form.token, "Token credential key", true);
      if (e) errors.fields.token = e;
      break;
    }
    case "basic": {
      const u = checkCredentialKey(form.username, "Username credential key", true);
      if (u) errors.fields.username = u;
      const p = checkCredentialKey(form.password, "Password credential key", false);
      if (p) errors.fields.password = p;
      break;
    }
    case "api-key": {
      const k = checkCredentialKey(form.apiKey, "API key credential", true);
      if (k) errors.fields.apiKey = k;
      const header = form.apiKeyHeader.trim();
      if (header && !HEADER_NAME_RE.test(header)) {
        errors.fields.apiKeyHeader = "Only letters, digits, and hyphens allowed";
      }
      break;
    }
    case "custom": {
      const seen = new Map<string, number>();
      for (const row of form.customHeaders) {
        const name = row.name.trim();
        if (!name) {
          errors.headerRows[row._id] = "Header name is required";
          continue;
        }
        if (!HEADER_NAME_RE.test(name)) {
          errors.headerRows[row._id] = "Only letters, digits, and hyphens allowed";
          continue;
        }
        if (!row.value.trim()) {
          errors.headerRows[row._id] = "Header value is required";
          continue;
        }
        const lower = name.toLowerCase();
        if (seen.has(lower)) {
          errors.headerRows[row._id] = "Duplicate header name";
        }
        seen.set(lower, row._id);
      }
      if (form.customHeaders.length === 0) {
        errors.fields.customHeaders = "Add at least one header";
      }
      break;
    }
    case "passthrough":
      break;
  }
  return errors;
}

export function validateSubs(form: ServiceFormState): StepErrors {
  const errors = emptyStepErrors();
  const seenKeys = new Map<string, number>();
  for (const row of form.subs) {
    // Fully-empty rows are dropped on submit — don't nag about them.
    if (!row.key.trim() && !row.placeholder.trim()) continue;

    const rowErrors: SubRowErrors = {};
    const key = row.key.trim();
    if (!key) {
      rowErrors.key = "Credential key is required";
    } else if (!CREDENTIAL_KEY_RE.test(key)) {
      rowErrors.key = "Must be UPPER_SNAKE_CASE";
    } else if (seenKeys.has(key)) {
      rowErrors.key = "Duplicate credential key";
    }
    seenKeys.set(key, row._id);

    const placeholder = row.placeholder.trim();
    if (!placeholder) {
      rowErrors.placeholder = "Placeholder is required";
    } else if (placeholder.length < 4) {
      rowErrors.placeholder = "Must be at least 4 characters";
    }

    if (row.env.trim() && !ENV_VAR_RE.test(row.env.trim())) {
      rowErrors.env = "Not a valid environment variable name";
    }

    if (row.in.length === 0) {
      rowErrors.surfaces = "Select at least one surface";
    }

    if (Object.keys(rowErrors).length > 0) errors.subRows[row._id] = rowErrors;
  }
  return errors;
}

/* -- Placeholder generation (mirrors broker.GeneratePlaceholder) -- */

const PLACEHOLDER_MARKER = "thisisaplaceholder";

export function generatePlaceholder(prefix: string, key: string, targetLen: number): string {
  if (!prefix) return `__${key || "NAME"}__`;
  const base = prefix + PLACEHOLDER_MARKER;
  if (targetLen <= base.length) return base;
  return base + "0".repeat(targetLen - base.length);
}

/* -- Name helpers -- */

// Appends a numeric suffix ("openai" -> "openai-2") until the name is unused.
export function uniqueServiceName(base: string, existingNames: string[]): string {
  const taken = new Set(existingNames);
  if (!taken.has(base)) return base;
  let i = 2;
  while (taken.has(`${base}-${i}`)) i += 1;
  return `${base}-${i}`;
}

/* -- Form builders -- */

export function blankForm(defaults?: {
  name?: string;
  host?: string;
  authScheme?: string;
  authHeader?: string;
}): ServiceFormState {
  const authType = (defaults?.authScheme as AuthType) || "passthrough";
  return {
    name: defaults?.name ?? "",
    host: defaults?.host ?? "",
    enabled: true,
    authType,
    token: "",
    username: "",
    password: "",
    apiKey: "",
    apiKeyHeader: authType === "api-key" ? defaults?.authHeader ?? "" : "",
    apiKeyPrefix: "",
    customHeaders: [newHeaderRow()],
    subs: [],
  };
}

export function formFromService(svc: Service): ServiceFormState {
  const authType = (svc.auth?.type as AuthType) || "passthrough";
  return {
    name: svc.name,
    host: svc.host,
    enabled: svc.enabled !== false,
    authType,
    token: svc.auth?.token ?? "",
    username: svc.auth?.username ?? "",
    password: svc.auth?.password ?? "",
    apiKey: svc.auth?.key ?? "",
    apiKeyHeader: svc.auth?.header ?? "",
    apiKeyPrefix: svc.auth?.prefix ?? "",
    customHeaders:
      svc.auth?.headers && Object.keys(svc.auth.headers).length > 0
        ? Object.entries(svc.auth.headers).map(([name, value]) => ({ _id: nextRowId(), name, value }))
        : [newHeaderRow()],
    subs: (svc.substitutions ?? []).map((s) => ({
      _id: nextRowId(),
      key: s.key,
      placeholder: s.placeholder,
      in: s.in && s.in.length > 0 ? [...s.in] : [...DEFAULT_SUBSTITUTION_SURFACES],
      env: s.env ?? "",
    })),
  };
}

// Seeds the form from a template. Credential key fields get the template's
// suggested key; everything else (host, headers, substitutions) comes
// prefilled so the user only has to confirm.
export function formFromTemplate(tpl: CatalogTemplate, existingNames: string[]): ServiceFormState {
  const authType = (tpl.auth_type as AuthType) || "passthrough";
  return {
    ...blankForm(),
    name: uniqueServiceName(tpl.id, existingNames),
    host: tpl.host,
    authType,
    token: authType === "bearer" ? tpl.suggested_credential_key : "",
    // Catalogued basic-auth services (Twilio, Jira) carry a token that belongs
    // in the password slot — the username (AccountSID, email) is user-specific.
    password: authType === "basic" ? tpl.suggested_credential_key : "",
    apiKey: authType === "api-key" ? tpl.suggested_credential_key : "",
    apiKeyHeader: authType === "api-key" ? tpl.header ?? "" : "",
    apiKeyPrefix: authType === "api-key" ? tpl.prefix ?? "" : "",
    customHeaders:
      authType === "custom" && tpl.headers && Object.keys(tpl.headers).length > 0
        ? Object.entries(tpl.headers).map(([name, value]) => ({ _id: nextRowId(), name, value }))
        : [newHeaderRow()],
    subs: (tpl.substitutions ?? []).map((s) => ({
      _id: nextRowId(),
      key: s.key,
      placeholder: s.placeholder,
      in: s.in && s.in.length > 0 ? [...s.in] : [...DEFAULT_SUBSTITUTION_SURFACES],
      env: s.env ?? "",
    })),
  };
}

// Default placeholder for a freshly-added substitution row: credential-shaped
// when the template carries shaping metadata, blank otherwise.
export function newSubRow(tpl?: CatalogTemplate | null): SubRow {
  return {
    _id: nextRowId(),
    key: "",
    placeholder: tpl?.placeholder_prefix
      ? generatePlaceholder(tpl.placeholder_prefix, "", tpl.placeholder_length ?? 0)
      : "",
    in: [...DEFAULT_SUBSTITUTION_SURFACES],
    env: "",
  };
}

export function newHeaderRow(): HeaderRow {
  return { _id: nextRowId(), name: "", value: "" };
}

/* -- Submit -- */

function buildAuth(form: ServiceFormState): Auth {
  switch (form.authType) {
    case "bearer":
      return { type: "bearer", token: form.token.trim() };
    case "basic": {
      const auth: Auth = { type: "basic", username: form.username.trim() };
      if (form.password.trim()) auth.password = form.password.trim();
      return auth;
    }
    case "api-key": {
      const auth: Auth = { type: "api-key", key: form.apiKey.trim() };
      if (form.apiKeyHeader.trim()) auth.header = form.apiKeyHeader.trim();
      if (form.apiKeyPrefix) auth.prefix = form.apiKeyPrefix;
      return auth;
    }
    case "custom": {
      const headers: Record<string, string> = {};
      for (const h of form.customHeaders) {
        if (h.name.trim()) headers[h.name.trim()] = h.value.trim();
      }
      return { type: "custom", headers };
    }
    case "passthrough":
    default:
      return { type: "passthrough" };
  }
}

// Send only `host` (inline-form accepted). The server splits into host +
// path on ingest — the UI never names a separate path field.
export function buildService(form: ServiceFormState): Service {
  const subs = form.subs
    .map((s) => ({
      key: s.key.trim(),
      placeholder: s.placeholder.trim(),
      in: s.in.length > 0 ? s.in : [...DEFAULT_SUBSTITUTION_SURFACES],
      ...(s.env.trim() ? { env: s.env.trim() } : {}),
    }))
    .filter((s) => s.key !== "" || s.placeholder !== "");

  return {
    name: form.name.trim(),
    host: form.host.trim(),
    ...(form.enabled ? {} : { enabled: false }),
    auth: buildAuth(form),
    ...(subs.length > 0 && { substitutions: subs }),
  };
}

/* -- Dirty tracking -- */

// Stable string identity for a form state; row _ids are part of state so
// untouched rows don't churn the comparison.
export const formIdentity = (form: ServiceFormState): string => JSON.stringify(form);
