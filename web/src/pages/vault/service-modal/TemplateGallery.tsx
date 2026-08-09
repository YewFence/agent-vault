import { useMemo, useState } from "react";
import { AuthTypeBadge, SectionLabel, TemplateIcon } from "./bits";
import { CATEGORY_ORDER, type CatalogTemplate } from "./types";

// Shown in the Popular section before the category sections.
const POPULAR_IDS = ["openai", "anthropic", "github", "stripe", "slack", "telegram"];

function TemplateCard({ template, onClick }: { template: CatalogTemplate; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex cursor-pointer flex-col gap-3 rounded-lg border border-border bg-surface-raised p-4 text-left transition-colors hover:border-border-focus hover:shadow-[0_0_0_3px_var(--color-primary-ring)]"
    >
      <div className="flex items-start justify-between gap-2">
        <TemplateIcon name={template.name} className="h-9 w-9 text-sm" />
        <AuthTypeBadge
          authType={template.auth_type}
          hasSubs={(template.substitutions?.length ?? 0) > 0}
        />
      </div>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-semibold text-text">{template.name}</p>
        <p className="text-xs leading-relaxed text-text-muted line-clamp-2">{template.description}</p>
      </div>
    </button>
  );
}

function CustomCard({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex cursor-pointer flex-col gap-3 rounded-lg border border-dashed border-border bg-transparent p-4 text-left transition-colors hover:border-border-focus hover:bg-surface-raised"
    >
      <div className="flex items-start gap-2">
        <div className="flex h-9 w-9 items-center justify-center rounded-md border border-border bg-surface-raised">
          <svg
            className="h-4 w-4 text-text-muted"
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
        </div>
      </div>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-semibold text-text">Custom</p>
        <p className="text-xs leading-relaxed text-text-muted">Set up any service manually.</p>
      </div>
    </button>
  );
}

function Grid({ children }: { children: React.ReactNode }) {
  return <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3">{children}</div>;
}

export default function TemplateGallery({
  catalog,
  onSelect,
}: {
  catalog: CatalogTemplate[];
  /** null = Custom / start from scratch. */
  onSelect: (template: CatalogTemplate | null) => void;
}) {
  const [search, setSearch] = useState("");

  const query = search.trim().toLowerCase();
  const isSearching = query.length > 0;

  const filtered = useMemo(() => {
    if (!query) return catalog;
    return catalog.filter((t) =>
      [t.name, t.id, t.description, t.category ?? "", ...(t.aliases ?? [])].some((term) =>
        term.toLowerCase().includes(query)
      )
    );
  }, [catalog, query]);

  const popular = useMemo(
    () =>
      POPULAR_IDS.map((id) => catalog.find((t) => t.id === id)).filter(
        (t): t is CatalogTemplate => Boolean(t)
      ),
    [catalog]
  );

  const byCategory = useMemo(() => {
    const present = new Set(catalog.map((t) => t.category || ""));
    const ordered = CATEGORY_ORDER.filter((c) => present.has(c));
    // Unknown/new categories sort after the declared ones, alphabetically.
    const extra = [...present]
      .filter((c) => !(CATEGORY_ORDER as readonly string[]).includes(c))
      .sort();
    return [...ordered, ...extra].map((category) => ({
      category,
      templates: catalog.filter((t) => (t.category || "") === category),
    }));
  }, [catalog]);

  return (
    <div className="flex flex-col gap-7">
      <div className="relative">
        <svg
          className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4 text-text-dim pointer-events-none"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="11" cy="11" r="8" />
          <line x1="21" y1="21" x2="16.65" y2="16.65" />
        </svg>
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search services — OpenAI, Stripe, Telegram, GitHub…"
          autoFocus
          className="w-full pl-10 pr-4 py-3 bg-surface-raised border border-border rounded-lg text-text text-sm outline-none transition-colors focus:border-border-focus focus:shadow-[0_0_0_3px_var(--color-primary-ring)]"
        />
      </div>

      {isSearching ? (
        <section>
          {filtered.length === 0 ? (
            <p className="text-sm text-text-muted py-6 text-center">
              No templates match "{search.trim()}" — start from Custom instead.
            </p>
          ) : (
            <Grid>
              <CustomCard onClick={() => onSelect(null)} />
              {filtered.map((template) => (
                <TemplateCard key={template.id} template={template} onClick={() => onSelect(template)} />
              ))}
            </Grid>
          )}
        </section>
      ) : (
        <>
          <section>
            <SectionLabel>Popular</SectionLabel>
            <Grid>
              <CustomCard onClick={() => onSelect(null)} />
              {popular.map((template) => (
                <TemplateCard key={template.id} template={template} onClick={() => onSelect(template)} />
              ))}
            </Grid>
          </section>
          {byCategory.map(
            ({ category, templates }) =>
              templates.length > 0 && (
                <section key={category}>
                  <SectionLabel>{category}</SectionLabel>
                  <Grid>
                    {templates.map((template) => (
                      <TemplateCard
                        key={template.id}
                        template={template}
                        onClick={() => onSelect(template)}
                      />
                    ))}
                  </Grid>
                </section>
              )
          )}
        </>
      )}

      <p className="text-xs text-text-dim">
        Don&apos;t see the service you&apos;re looking for?{" "}
        <a
          target="_blank"
          rel="noopener noreferrer"
          href="https://github.com/Infisical/agent-vault/issues"
          className="underline underline-offset-2 hover:text-text-muted"
        >
          Request it on GitHub
        </a>
        .
      </p>
    </div>
  );
}
