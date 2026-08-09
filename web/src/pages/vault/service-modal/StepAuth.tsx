import FormField from "../../../components/FormField";
import Input from "../../../components/Input";
import SegmentedTabs from "../../../components/SegmentedTabs";
import CredentialKeyInput from "./CredentialKeyInput";
import { FieldErrorText, IconButton } from "./bits";
import { newHeaderRow } from "./serviceForm";
import {
  AUTH_TYPE_OPTIONS,
  type AuthType,
  type ServiceFormState,
} from "./types";

export default function StepAuth({
  form,
  patch,
  errors,
  headerRowErrors,
  credentialKeys,
}: {
  form: ServiceFormState;
  patch: (partial: Partial<ServiceFormState>) => void;
  errors: Record<string, string>;
  headerRowErrors: Record<number, string>;
  credentialKeys: string[];
}) {
  const setHeaderRow = (id: number, partial: Partial<{ name: string; value: string }>) =>
    patch({
      customHeaders: form.customHeaders.map((h) => (h._id === id ? { ...h, ...partial } : h)),
    });

  return (
    <div className="flex flex-col gap-5">
      <SegmentedTabs
        options={AUTH_TYPE_OPTIONS}
        value={form.authType}
        onChange={(v) => patch({ authType: v as AuthType })}
        ariaLabel="Authentication method"
      />

      {form.authType === "bearer" && (
        <FormField
          label="Token Credential Key"
          tooltip="The UPPER_SNAKE_CASE name of the credential storing the token. Pick an existing key or type a new one."
          required
          error={errors.token}
        >
          <CredentialKeyInput
            value={form.token}
            onChange={(v) => patch({ token: v })}
            credentialKeys={credentialKeys}
            placeholder="e.g. STRIPE_KEY"
            error={Boolean(errors.token)}
          />
        </FormField>
      )}

      {form.authType === "basic" && (
        <>
          <FormField
            label="Username Credential Key"
            tooltip="Credential key for the Basic Auth username."
            required
            error={errors.username}
          >
            <CredentialKeyInput
              value={form.username}
              onChange={(v) => patch({ username: v })}
              credentialKeys={credentialKeys}
              placeholder="e.g. ASHBY_API_KEY"
              error={Boolean(errors.username)}
            />
          </FormField>
          <FormField
            label="Password Credential Key"
            tooltip="Optional — leave empty if the service only requires a username."
            error={errors.password}
          >
            <CredentialKeyInput
              value={form.password}
              onChange={(v) => patch({ password: v })}
              credentialKeys={credentialKeys}
              placeholder="e.g. ASHBY_PASSWORD"
              error={Boolean(errors.password)}
            />
          </FormField>
        </>
      )}

      {form.authType === "api-key" && (
        <>
          <FormField
            label="API Key Credential"
            tooltip="The UPPER_SNAKE_CASE name of the credential storing the API key."
            required
            error={errors.apiKey}
          >
            <CredentialKeyInput
              value={form.apiKey}
              onChange={(v) => patch({ apiKey: v })}
              credentialKeys={credentialKeys}
              placeholder="e.g. OPENAI_API_KEY"
              error={Boolean(errors.apiKey)}
            />
          </FormField>
          <div className="grid grid-cols-2 gap-3">
            <FormField
              label="Header Name"
              tooltip="Which header to inject. Defaults to Authorization."
              error={errors.apiKeyHeader}
            >
              <Input
                placeholder="Authorization"
                value={form.apiKeyHeader}
                onChange={(e) => patch({ apiKeyHeader: e.target.value })}
                error={Boolean(errors.apiKeyHeader)}
              />
            </FormField>
            <FormField label="Prefix" tooltip='Optional prefix before the key value (e.g. "Bearer ").'>
              <Input
                placeholder="e.g. Bearer "
                value={form.apiKeyPrefix}
                onChange={(e) => patch({ apiKeyPrefix: e.target.value })}
              />
            </FormField>
          </div>
        </>
      )}

      {form.authType === "passthrough" && (
        <div className="rounded-lg border border-border bg-bg p-3 text-sm text-text-muted leading-relaxed">
          Passthrough allowlists the host without injecting a credential —
          same header forwarding as every other auth type, just with no
          broker-injected auth header on top. Use this when the agent
          already holds the credential.
        </div>
      )}

      {form.authType === "custom" && (
        <FormField
          label="Headers"
          tooltip="Type {{ CREDENTIAL_KEY }} to reference a stored credential."
          required
        >
          <div className="space-y-3">
            {form.customHeaders.map((header) => (
              <div key={header._id}>
                <div className="flex gap-3 items-center">
                  <Input
                    placeholder="Header name"
                    value={header.name}
                    onChange={(e) => setHeaderRow(header._id, { name: e.target.value })}
                    error={Boolean(headerRowErrors[header._id])}
                  />
                  <Input
                    placeholder="e.g. Bearer {{ STRIPE_KEY }}"
                    value={header.value}
                    onChange={(e) => setHeaderRow(header._id, { value: e.target.value })}
                    error={Boolean(headerRowErrors[header._id])}
                    className="font-mono"
                  />
                  {form.customHeaders.length > 1 && (
                    <IconButton
                      onClick={() =>
                        patch({ customHeaders: form.customHeaders.filter((h) => h._id !== header._id) })
                      }
                      ariaLabel="Remove header"
                    />
                  )}
                </div>
                <FieldErrorText>{headerRowErrors[header._id]}</FieldErrorText>
              </div>
            ))}
            {errors.customHeaders && <FieldErrorText>{errors.customHeaders}</FieldErrorText>}
            <button
              type="button"
              onClick={() =>
                patch({ customHeaders: [...form.customHeaders, newHeaderRow()] })
              }
              className="text-sm font-medium text-primary hover:text-primary-hover transition-colors"
            >
              + Add another
            </button>
          </div>
        </FormField>
      )}
    </div>
  );
}
