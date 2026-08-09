import { useMemo, useState } from "react";
import Button from "../../../components/Button";
import { ErrorBanner } from "../../../components/shared";
import { SectionLabel } from "./bits";
import {
  buildService,
  emptyStepErrors,
  hasStepErrors,
  validateAuth,
  validateDetails,
  validateSubs,
  type StepErrors,
} from "./serviceForm";
import StepAuth from "./StepAuth";
import StepDetails from "./StepDetails";
import StepSubstitutions from "./StepSubstitutions";
import type {
  CatalogTemplate,
  Service,
  ServiceFormState,
} from "./types";

type SectionId = "details" | "auth" | "subs";
const SECTION_ORDER: SectionId[] = ["details", "auth", "subs"];

export default function ServiceEditor({
  mode,
  form,
  onFormChange,
  template,
  existingNames,
  credentialKeys,
  dirty,
  onSave,
  onBackToTemplates,
}: {
  mode: "add" | "edit";
  form: ServiceFormState;
  onFormChange: (next: ServiceFormState) => void;
  /** Selected template; null for custom/edit/unknown. */
  template: CatalogTemplate | null;
  /** Names already taken in this vault (excludes the service being edited). */
  existingNames: string[];
  credentialKeys: string[];
  dirty: boolean;
  onSave: (service: Service) => Promise<void>;
  /** Add mode only — returning to the template gallery. */
  onBackToTemplates?: () => void;
}) {
  // Field errors stay hidden until the first submit attempt.
  const [submitAttempted, setSubmitAttempted] = useState(false);
  const [saving, setSaving] = useState(false);
  const [serverError, setServerError] = useState("");

  const patch = (partial: Partial<ServiceFormState>) => onFormChange({ ...form, ...partial });

  // Validators are cheap — run them every render so errors re-validate live
  // as the user edits after the first submit attempt.
  const errors: Record<SectionId, StepErrors> = useMemo(
    () => ({
      details: validateDetails(form, existingNames),
      auth: validateAuth(form),
      subs: validateSubs(form),
    }),
    [form, existingNames]
  );
  const shown = submitAttempted
    ? errors
    : { details: emptyStepErrors(), auth: emptyStepErrors(), subs: emptyStepErrors() };

  const submit = async () => {
    setSaving(true);
    setServerError("");
    try {
      await onSave(buildService(form));
    } catch (err: unknown) {
      setServerError(err instanceof Error ? err.message : "An error occurred.");
      setSaving(false);
    }
  };

  const handleSubmit = () => {
    setSubmitAttempted(true);
    const firstInvalid = SECTION_ORDER.find((id) => hasStepErrors(errors[id]));
    if (firstInvalid) {
      document
        .getElementById(`service-section-${firstInvalid}`)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
      return;
    }
    void submit();
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* Single scrolling form — every section visible at once */}
      <div
        className="flex min-h-0 flex-1 flex-col gap-10 overflow-y-auto px-6 py-6"
        style={{ scrollbarWidth: "thin", scrollbarColor: "var(--color-border) transparent" }}
      >
        <section id="service-section-details">
          <SectionLabel>Service details</SectionLabel>
          <StepDetails form={form} patch={patch} errors={shown.details.fields} />
        </section>

        <section id="service-section-auth">
          <SectionLabel>Authentication</SectionLabel>
          <StepAuth
            form={form}
            patch={patch}
            errors={shown.auth.fields}
            headerRowErrors={shown.auth.headerRows}
            credentialKeys={credentialKeys}
          />
        </section>

        <section id="service-section-subs">
          <SectionLabel>Substitutions</SectionLabel>
          <StepSubstitutions
            form={form}
            patch={patch}
            subRowErrors={shown.subs.subRows}
            credentialKeys={credentialKeys}
            template={template}
          />
        </section>

        {serverError && <ErrorBanner message={serverError} />}
      </div>

      {/* Footer */}
      <div className="flex shrink-0 items-center justify-between gap-3 border-t border-border px-6 py-4">
        <div className="flex items-center gap-4">
          {onBackToTemplates && (
            <button
              type="button"
              onClick={onBackToTemplates}
              disabled={saving}
              className="text-sm text-text-muted hover:text-text transition-colors"
            >
              ← Back to templates
            </button>
          )}
          <span className="text-xs text-text-muted">{dirty ? "Unsaved changes" : ""}</span>
        </div>
        <Button onClick={handleSubmit} loading={saving} disabled={saving}>
          {mode === "add" ? "Add service" : "Save changes"}
        </Button>
      </div>
    </div>
  );
}
