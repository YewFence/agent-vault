import FormField from "../../../components/FormField";
import Input from "../../../components/Input";
import Toggle from "../../../components/Toggle";
import type { ServiceFormState } from "./types";

export default function StepDetails({
  form,
  patch,
  errors,
}: {
  form: ServiceFormState;
  patch: (partial: Partial<ServiceFormState>) => void;
  /** Shown only after the user has attempted to submit. */
  errors: Record<string, string>;
}) {
  return (
    <div className="flex flex-col gap-5">
      <FormField
        label="Name"
        tooltip="Slug-style identifier (3–64 chars, lowercase, hyphens). The canonical per-vault key for this service."
        required
        error={errors.name}
      >
        <Input
          placeholder="e.g. stripe, slack-bot, internal-billing"
          value={form.name}
          onChange={(e) => patch({ name: e.target.value })}
          error={Boolean(errors.name)}
          autoFocus
        />
      </FormField>

      <FormField
        label="Host Pattern"
        tooltip="Host with optional port and path glob. Omit port to match any port. * is a subdomain label in the host (*.github.com) and a greedy glob in the path (/api/*). Examples: api.stripe.com, internal.corp.com:3000, slack.com/api/*."
        required
        error={errors.host}
      >
        <Input
          placeholder="e.g. api.stripe.com, internal.corp.com:3000, or slack.com/api/*"
          value={form.host}
          onChange={(e) => patch({ host: e.target.value })}
          error={Boolean(errors.host)}
          className="font-mono"
        />
      </FormField>

      <div className="flex items-start justify-between gap-4 rounded-lg border border-border px-4 py-3">
        <div className="min-w-0">
          <div className="text-sm font-medium text-text">Enabled</div>
          <div className="text-xs text-text-muted mt-0.5">
            Disabled services return 403 until re-enabled.
          </div>
        </div>
        <Toggle checked={form.enabled} onChange={(v) => patch({ enabled: v })} ariaLabel="Enabled" />
      </div>
    </div>
  );
}
