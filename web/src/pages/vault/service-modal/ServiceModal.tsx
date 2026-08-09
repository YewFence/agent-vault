import { useEffect, useState } from "react";
import Button from "../../../components/Button";
import Modal from "../../../components/Modal";
import Sheet from "../../../components/Sheet";
import { apiRequest } from "../../../lib/api";
import {
  blankForm,
  formFromService,
  formFromTemplate,
  formIdentity,
} from "./serviceForm";
import ServiceEditor from "./ServiceEditor";
import TemplateGallery from "./TemplateGallery";
import type { CatalogTemplate, Service, ServiceFormState } from "./types";

/** What the discard-confirmation dialog will do when confirmed. */
type ConfirmAction = "close" | "back-to-templates";

export default function ServiceModal({
  title,
  initial,
  defaultHost,
  defaultName,
  defaultAuthScheme,
  defaultAuthHeader,
  defaultPreset,
  catalog,
  existingNames,
  vaultName,
  onClose,
  onSave,
}: {
  title: string;
  /** Set for edit mode; gallery and template step are skipped. */
  initial?: Service;
  defaultHost?: string;
  defaultName?: string;
  defaultAuthScheme?: string;
  defaultAuthHeader?: string;
  /** Template id to pre-apply (from the catalog "Use" link). */
  defaultPreset?: string;
  catalog: CatalogTemplate[];
  /** All service names in the vault (the edited one is excluded internally). */
  existingNames: string[];
  vaultName: string;
  onClose: () => void;
  onSave: (service: Service) => Promise<void>;
}) {
  const isEdit = Boolean(initial);
  const presetTemplate = defaultPreset
    ? catalog.find((t) => t.id === defaultPreset) ?? null
    : null;
  // Discovered-host and preset entries land directly in the form; everyone
  // else picks a template first. An empty catalog degrades to manual entry.
  const startsInForm = isEdit || Boolean(defaultHost) || Boolean(presetTemplate) || catalog.length === 0;

  const [phase, setPhase] = useState<"gallery" | "form">(startsInForm ? "form" : "gallery");
  const [template, setTemplate] = useState<CatalogTemplate | null>(presetTemplate);
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(null);

  // Names taken by *other* services — the duplicate-name check's corpus.
  const otherNames = existingNames.filter((n) => n !== initial?.name);

  const [form, setForm] = useState<ServiceFormState>(() => {
    if (initial) return formFromService(initial);
    if (presetTemplate) return formFromTemplate(presetTemplate, otherNames);
    return blankForm({
      name: defaultName,
      host: defaultHost,
      authScheme: defaultAuthScheme,
      authHeader: defaultAuthHeader,
    });
  });
  const [snapshot, setSnapshot] = useState(() => formIdentity(form));
  const dirty = formIdentity(form) !== snapshot;

  const [credentialKeys, setCredentialKeys] = useState<string[]>([]);
  useEffect(() => {
    let live = true;
    // Vaults backed by an external credential store reject this call —
    // degrade to free-text entry with no suggestions.
    apiRequest<{ keys?: string[] }>(`/v1/credentials?vault=${encodeURIComponent(vaultName)}`)
      .then((data) => {
        if (live) setCredentialKeys(data.keys ?? []);
      })
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [vaultName]);

  const selectTemplate = (tpl: CatalogTemplate | null) => {
    const next = tpl ? formFromTemplate(tpl, otherNames) : blankForm();
    setTemplate(tpl);
    setForm(next);
    setSnapshot(formIdentity(next));
    setPhase("form");
  };

  const backToTemplates = () => setPhase("gallery");

  // Closing (Esc/backdrop/X) and backing out to templates both route here —
  // a dirty form asks for confirmation first.
  const requestClose = () => {
    if (confirmAction) return; // the confirm dialog owns dismissal right now
    if (phase === "form" && dirty) {
      setConfirmAction("close");
      return;
    }
    onClose();
  };

  const requestBackToTemplates = () => {
    if (dirty) {
      setConfirmAction("back-to-templates");
      return;
    }
    backToTemplates();
  };

  const confirmDiscard = () => {
    const action = confirmAction;
    setConfirmAction(null);
    if (action === "back-to-templates") {
      backToTemplates();
    } else {
      onClose();
    }
  };

  const sheetTitle =
    phase === "gallery"
      ? title
      : isEdit
        ? title
        : (template?.name ?? "Custom service");

  return (
    <>
      <Sheet
        open
        onClose={requestClose}
        eyebrow={phase === "gallery" ? "Service" : isEdit ? "Edit service" : "Add service"}
        title={sheetTitle}
        widthClass={phase === "gallery" ? "max-w-[1080px]" : "max-w-3xl"}
        bodyClassName={phase === "form" ? "flex-1 overflow-hidden flex flex-col" : undefined}
        headerExtra={
          phase === "form" && template ? (
            <p className="text-sm text-text-muted">
              {template.description} · <span className="font-mono text-[13px]">{template.host}</span>
            </p>
          ) : undefined
        }
      >
        {phase === "gallery" ? (
          <TemplateGallery catalog={catalog} onSelect={selectTemplate} />
        ) : (
          <ServiceEditor
            mode={isEdit ? "edit" : "add"}
            form={form}
            onFormChange={setForm}
            template={template}
            existingNames={otherNames}
            credentialKeys={credentialKeys}
            dirty={dirty}
            onSave={onSave}
            onBackToTemplates={isEdit ? undefined : requestBackToTemplates}
          />
        )}
      </Sheet>

      <Modal
        open={confirmAction !== null}
        onClose={() => setConfirmAction(null)}
        title="Discard changes?"
        description="Your progress configuring this service will be lost."
        footer={
          <>
            <Button variant="secondary" onClick={() => setConfirmAction(null)}>
              Keep editing
            </Button>
            <Button
              onClick={confirmDiscard}
              className="!bg-danger !text-white hover:!bg-danger/90"
            >
              Discard
            </Button>
          </>
        }
      >
        <></>
      </Modal>
    </>
  );
}
