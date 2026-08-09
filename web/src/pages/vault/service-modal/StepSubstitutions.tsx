import {
  SUBSTITUTION_SURFACES,
} from "../../../components/ProposalPreview";
import FormField from "../../../components/FormField";
import Input from "../../../components/Input";
import CredentialKeyInput from "./CredentialKeyInput";
import { IconButton } from "./bits";
import { newSubRow, type SubRowErrors } from "./serviceForm";
import type { CatalogTemplate, ServiceFormState, SubRow } from "./types";

function SurfaceChips({
  row,
  onChange,
  error,
}: {
  row: SubRow;
  onChange: (next: string[]) => void;
  error?: boolean;
}) {
  return (
    <span className="inline-flex flex-wrap items-center gap-1.5">
      {SUBSTITUTION_SURFACES.map((surface) => {
        const checked = row.in.includes(surface);
        return (
          <button
            key={surface}
            type="button"
            role="switch"
            aria-checked={checked}
            onClick={() => {
              const current = new Set(row.in);
              if (current.has(surface)) current.delete(surface);
              else current.add(surface);
              onChange(SUBSTITUTION_SURFACES.filter((sf) => current.has(sf)));
            }}
            className={`px-2.5 py-1 rounded-md font-mono text-xs border transition-colors ${
              checked
                ? "border-primary text-primary bg-[var(--color-primary-ring)]"
                : `${error ? "border-danger/60" : "border-border"} text-text-dim hover:text-text-muted`
            }`}
          >
            {surface}
          </button>
        );
      })}
    </span>
  );
}

export default function StepSubstitutions({
  form,
  patch,
  subRowErrors,
  credentialKeys,
  template,
}: {
  form: ServiceFormState;
  patch: (partial: Partial<ServiceFormState>) => void;
  subRowErrors: Record<number, SubRowErrors>;
  credentialKeys: string[];
  template?: CatalogTemplate | null;
}) {
  const setSub = (id: number, partial: Partial<SubRow>) =>
    patch({ subs: form.subs.map((s) => (s._id === id ? { ...s, ...partial } : s)) });

  return (
    <div className="flex flex-col gap-3">
      <p className="text-xs text-text-muted leading-relaxed">
        The broker rewrites the placeholder in the selected surfaces with the
        credential&apos;s value before forwarding the request. Substitution
        composes with any auth type — including passthrough.
      </p>

      {form.subs.length === 0 && (
        <div className="rounded-lg border border-border bg-bg/50 p-4 text-center text-sm text-text-muted">
          No substitutions configured. Add one below if the credential travels
          in the URL or the agent&apos;s client checks its format.
        </div>
      )}

      {form.subs.map((sub) => {
        const errors = subRowErrors[sub._id] ?? {};
        return (
          <div
            key={sub._id}
            className="rounded-lg border border-border bg-bg p-4 flex items-start gap-3"
          >
            <div className="flex-1 min-w-0 flex flex-col gap-4">
              <FormField
                label="Replace"
                tooltip="The exact string the agent sends in place of the real credential."
                error={errors.placeholder}
              >
                <Input
                  className="font-mono"
                  placeholder="__placeholder__"
                  value={sub.placeholder}
                  onChange={(e) => setSub(sub._id, { placeholder: e.target.value })}
                  error={Boolean(errors.placeholder)}
                />
              </FormField>

              <FormField
                label="In surfaces"
                tooltip="Which parts of the request get the swap."
                error={errors.surfaces}
              >
                <SurfaceChips
                  row={sub}
                  onChange={(next) => setSub(sub._id, { in: next })}
                  error={Boolean(errors.surfaces)}
                />
              </FormField>

              <FormField
                label="With value of"
                tooltip="The credential whose real value replaces the placeholder."
                error={errors.key}
              >
                <CredentialKeyInput
                  value={sub.key}
                  onChange={(v) => setSub(sub._id, { key: v })}
                  credentialKeys={credentialKeys}
                  placeholder="CREDENTIAL_KEY"
                  error={Boolean(errors.key)}
                />
              </FormField>

              <FormField
                label="Inject as (optional)"
                tooltip="vault run exports the placeholder to the agent through this environment variable."
                error={errors.env}
              >
                <Input
                  className="font-mono"
                  placeholder="ENV_NAME"
                  value={sub.env}
                  onChange={(e) => setSub(sub._id, { env: e.target.value })}
                  error={Boolean(errors.env)}
                />
              </FormField>
            </div>
            <IconButton
              onClick={() => patch({ subs: form.subs.filter((s) => s._id !== sub._id) })}
              ariaLabel="Remove substitution"
            />
          </div>
        );
      })}

      <div>
        <button
          type="button"
          onClick={() => patch({ subs: [...form.subs, newSubRow(template)] })}
          className="text-sm font-medium text-primary hover:text-primary-hover transition-colors"
        >
          + Add substitution
        </button>
      </div>
    </div>
  );
}
