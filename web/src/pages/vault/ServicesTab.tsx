import { useState, useEffect, useRef } from "react";
import { useSearch } from "@tanstack/react-router";
import {
  useVaultParams,
  LoadingSpinner,
  ErrorBanner,
  timeAgo,
} from "./shared";
import DropdownMenu from "../../components/DropdownMenu";
import DataTable, { type Column } from "../../components/DataTable";
import Modal from "../../components/Modal";
import Button from "../../components/Button";
import Toggle from "../../components/Toggle";
import { AUTH_TYPE_LABELS } from "../../components/ProposalPreview";
import { apiFetch, apiRequest } from "../../lib/api";
import { ServiceModal, type CatalogTemplate, type Service } from "./service-modal";

function isEnabled(service: Service): boolean {
  return service.enabled !== false;
}

function slugifyHost(host: string): string {
  let slug = host
    .toLowerCase()
    .replace(/[^a-z0-9]/g, "-")
    .replace(/-{2,}/g, "-")
    .replace(/^-|-$/g, "");
  if (slug.length > 64) slug = slug.slice(0, 64).replace(/-$/, "");
  if (slug.length < 3) slug = slug || "svc";
  return slug;
}

export default function ServicesTab() {
  const { vaultName, vaultRole } = useVaultParams();
  const { preset: presetParam } = useSearch({ strict: false }) as { preset?: string };
  const [services, setServices] = useState<Service[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [catalog, setCatalog] = useState<CatalogTemplate[]>([]);
  const presetApplied = useRef(false);

  // Add/Edit modal state: null = closed, -1 = add, 0+ = edit index
  const [editingIndex, setEditingIndex] = useState<number | null>(null);

  // Delete confirmation modal state
  const [deleteIndex, setDeleteIndex] = useState<number | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState("");

  // Discovered hosts state
  const [discoveredHosts, setDiscoveredHosts] = useState<
    { host: string; request_count: number; last_seen: string; auth_scheme?: string; auth_header?: string }[]
  >([]);
  const [discoveredTotal, setDiscoveredTotal] = useState(0);
  const [discoveredExpanded, setDiscoveredExpanded] = useState(false);
  const [discoveredCollapsed, setDiscoveredCollapsed] = useState(false);
  const [addWithHost, setAddWithHost] = useState<{ host: string; authScheme?: string; authHeader?: string } | null>(null);

  useEffect(() => {
    fetchServices();
    fetchCatalog();
    fetchDiscoveredHosts();
  }, []);

  useEffect(() => {
    if (presetParam && catalog.length > 0 && !presetApplied.current) {
      const match = catalog.find((t) => t.id === presetParam);
      if (match) {
        presetApplied.current = true;
        setEditingIndex(-1);
      }
    }
  }, [presetParam, catalog]);

  async function fetchCatalog() {
    try {
      const data = await apiRequest<{ services: CatalogTemplate[] }>("/v1/service-catalog");
      const entries = data.services ?? [];
      entries.sort((a, b) => a.name.localeCompare(b.name));
      setCatalog(entries);
    } catch {
      // Catalog is optional — degrade silently to manual entry.
    }
  }

  async function fetchServices() {
    try {
      const resp = await apiFetch(
        `/v1/vaults/${encodeURIComponent(vaultName)}/services`
      );
      if (resp.ok) {
        const data = await resp.json();
        setServices(data.services ?? []);
      } else {
        const data = await resp.json();
        setError(data.error || "Failed to load services.");
      }
    } catch {
      setError("Network error.");
    } finally {
      setLoading(false);
    }
  }

  async function fetchDiscoveredHosts(limit = 5) {
    try {
      const resp = await apiFetch(
        `/v1/vaults/${encodeURIComponent(vaultName)}/discovered-hosts?limit=${limit}`
      );
      if (resp.ok) {
        const data = await resp.json();
        setDiscoveredHosts(data.hosts ?? []);
        setDiscoveredTotal(data.total ?? 0);
      }
    } catch {
      // Discovered hosts are supplementary; degrade silently.
    }
  }

  async function saveServices(updatedServices: Service[]) {
    const resp = await apiFetch(
      `/v1/vaults/${encodeURIComponent(vaultName)}/services`,
      {
        method: "PUT",
        body: JSON.stringify({ services: updatedServices }),
      }
    );
    if (!resp.ok) {
      const data = await resp.json();
      throw new Error(data.error || "Failed to save services.");
    }
    // Re-fetch so the local copy always reflects exactly what the
    // server stored (e.g. inline-host re-joining for the read surface).
    await fetchServices();
    fetchDiscoveredHosts(discoveredExpanded ? 100 : 5);
  }

  async function toggleEnabled(index: number, next: boolean) {
    const service = services[index];
    if (!service) return;
    const applyEnabled = (want: boolean) => (list: Service[]) =>
      list.map((s) => (s.name === service.name ? { ...s, enabled: want } : s));
    setServices(applyEnabled(next));
    try {
      const resp = await apiFetch(
        `/v1/vaults/${encodeURIComponent(vaultName)}/services/${encodeURIComponent(service.name)}`,
        {
          method: "PATCH",
          body: JSON.stringify({ enabled: next }),
        }
      );
      if (!resp.ok) {
        const data = await resp.json();
        throw new Error(data.error || "Failed to update service.");
      }
    } catch (err: unknown) {
      setServices(applyEnabled(!next));
      setError(err instanceof Error ? err.message : "Failed to update service.");
    }
  }

  async function handleDelete() {
    if (deleteIndex === null) return;
    setDeleting(true);
    setDeleteError("");
    const updated = services.filter((_, i) => i !== deleteIndex);
    try {
      await saveServices(updated);
      setDeleteIndex(null);
    } catch (err: unknown) {
      setDeleteError(err instanceof Error ? err.message : "An error occurred.");
    } finally {
      setDeleting(false);
    }
  }

  const isAdmin = vaultRole === "admin";

  const columns: Column<Service>[] = [
    {
      key: "name",
      header: "Service",
      render: (service) => (
        <div>
          <div className="text-sm font-semibold text-text">{service.name}</div>
          <div className="text-xs text-text-muted mt-0.5">{service.host}</div>
        </div>
      ),
    },
    {
      key: "auth",
      header: "Auth",
      render: (service) => {
        const label = AUTH_TYPE_LABELS[service.auth?.type] || service.auth?.type || "\u2014";
        const subCount = service.substitutions?.length ?? 0;
        return (
          <div className="text-sm text-text">
            {label}
            {subCount > 0 && (
              <span className="ml-2 text-xs text-text-muted">
                + {subCount} substitution{subCount === 1 ? "" : "s"}
              </span>
            )}
          </div>
        );
      },
    },
    {
      key: "enabled",
      header: "Enabled",
      render: (service, index) => (
        <Toggle
          checked={isEnabled(service)}
          disabled={!isAdmin}
          onChange={(next) => toggleEnabled(index, next)}
          ariaLabel={`Toggle ${service.name}`}
        />
      ),
    },
    ...(isAdmin
      ? [
          {
            key: "actions",
            header: "",
            align: "right" as const,
            render: (_service: Service, index: number) => (
              <DropdownMenu
                items={[
                  { label: "Edit", onClick: () => setEditingIndex(index) },
                  { label: "Delete", onClick: () => setDeleteIndex(index), variant: "danger" },
                ]}
              />
            ),
          } as Column<Service>,
        ]
      : []),
  ];

  return (
    <div className="p-8 w-full max-w-[960px]">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h2 className="text-[22px] font-semibold text-text tracking-tight mb-1">
            Services
          </h2>
          <p className="text-sm text-text-muted">
            Define allowed hosts and configure authentication methods.
          </p>
        </div>
        {isAdmin && (
          <Button onClick={() => setEditingIndex(-1)}>
            <svg
              className="w-4 h-4"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <line x1="12" y1="5" x2="12" y2="19" />
              <line x1="5" y1="12" x2="19" y2="12" />
            </svg>
            Add service
          </Button>
        )}
      </div>

      {discoveredTotal > 0 && !loading && (
        <div className="mb-6 rounded-lg border border-warning/20 bg-warning-bg">
          <button
            type="button"
            className="flex w-full items-center justify-between px-4 py-3 text-left"
            onClick={() => setDiscoveredCollapsed((c) => !c)}
          >
            <span className="flex items-center gap-2 text-sm font-medium text-warning">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none"><circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="1.5"/><path d="M8 5v3M8 10h.01" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/></svg>
              {discoveredTotal} {discoveredTotal === 1 ? "host" : "hosts"} detected in recent traffic
            </span>
            <svg
              className={`w-4 h-4 text-text-muted transition-transform ${discoveredCollapsed ? "" : "rotate-180"}`}
              viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
            >
              <polyline points="6 9 12 15 18 9" />
            </svg>
          </button>
          {!discoveredCollapsed && (
            <div className="border-t border-info/20 px-4 pb-3">
              {discoveredHosts.map((dh) => (
                <div key={dh.host} className="flex items-center justify-between py-2.5 border-b border-border last:border-b-0">
                  <div>
                    <div className="font-mono text-sm text-text">{dh.host}</div>
                    <div className="text-xs text-text-muted mt-0.5">
                      {dh.request_count} {dh.request_count === 1 ? "request" : "requests"} &middot; {timeAgo(dh.last_seen)}
                    </div>
                  </div>
                  {isAdmin && (
                    <button
                      type="button"
                      className="rounded border border-border bg-surface px-2.5 py-1 text-xs text-text-muted hover:bg-surface-hover hover:text-text transition-colors"
                      onClick={() => {
                        setAddWithHost({ host: dh.host, authScheme: dh.auth_scheme, authHeader: dh.auth_header });
                        setEditingIndex(-1);
                      }}
                    >
                      Add as service
                    </button>
                  )}
                </div>
              ))}
              {discoveredTotal > 5 && !discoveredExpanded && (
                <button
                  type="button"
                  className="mt-2 text-xs text-warning hover:text-warning/80"
                  onClick={() => {
                    setDiscoveredExpanded(true);
                    fetchDiscoveredHosts(100);
                  }}
                >
                  Show all ({discoveredTotal})
                </button>
              )}
            </div>
          )}
        </div>
      )}

      {loading ? (
        <LoadingSpinner />
      ) : error ? (
        <ErrorBanner message={error} />
      ) : (
        <DataTable
          columns={columns}
          data={services}
          rowKey={(s) => s.name}
          emptyTitle="No services configured"
          emptyDescription="Add a service to allow agents to proxy requests through this vault."
        />
      )}

      {/* Delete confirmation modal */}
      <Modal
        open={deleteIndex !== null}
        onClose={() => {
          setDeleteIndex(null);
          setDeleteError("");
        }}
        title="Delete service"
        description={
          deleteIndex !== null && services[deleteIndex]
            ? `Permanently delete "${services[deleteIndex].name}" (${services[deleteIndex].host}). Agents will no longer be able to proxy requests through this service.`
            : "Permanently delete this service."
        }
        footer={
          <>
            <Button variant="secondary" onClick={() => setDeleteIndex(null)}>
              Cancel
            </Button>
            <Button
              onClick={handleDelete}
              loading={deleting}
              className="!bg-danger !text-white hover:!bg-danger/90"
            >
              Delete
            </Button>
          </>
        }
      >
        {deleteError && <ErrorBanner message={deleteError} />}
      </Modal>

      {editingIndex !== null && (
        <ServiceModal
          title={editingIndex === -1 ? "Add Service" : "Edit Service"}
          initial={editingIndex >= 0 ? services[editingIndex] : undefined}
          defaultHost={editingIndex === -1 ? addWithHost?.host : undefined}
          defaultName={editingIndex === -1 && addWithHost ? slugifyHost(addWithHost.host) : undefined}
          defaultAuthScheme={editingIndex === -1 ? addWithHost?.authScheme : undefined}
          defaultAuthHeader={editingIndex === -1 ? addWithHost?.authHeader : undefined}
          defaultPreset={editingIndex === -1 && !addWithHost ? presetParam : undefined}
          catalog={catalog}
          existingNames={services.map((s) => s.name)}
          vaultName={vaultName}
          onClose={() => {
            setEditingIndex(null);
            setAddWithHost(null);
          }}
          onSave={async (service) => {
            const updated = [...services];
            if (editingIndex === -1) {
              updated.push(service);
            } else {
              updated[editingIndex] = service;
            }
            await saveServices(updated);
            setEditingIndex(null);
            setAddWithHost(null);
          }}
        />
      )}
    </div>
  );
}
