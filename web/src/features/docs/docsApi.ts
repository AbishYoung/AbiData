import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { components } from "@/generated/api";
import type { DocSummary } from "./docsSearch";

export type DocsPage = components["schemas"]["Page"];

export const docsKeys = {
  index: ["docs", "index"] as const,
  page: (slug: string) => ["docs", "page", slug] as const,
};

async function fetchIndex(): Promise<DocSummary[]> {
  const body = await api.requestData((c, init) => c.GET("/api/v1/docs", init), {
    class: "background",
    label: "Loading documentation",
  });
  return body.pages ?? [];
}

async function fetchPage(slug: string): Promise<DocsPage> {
  return api.requestData((c, init) => c.GET("/api/v1/docs/page", { ...init, params: { query: { slug } } }), {
    class: "background",
    label: "Loading documentation page",
  });
}

/** The page list with search text. Docs only change with a new server version, so it is fetched once. */
export function useDocsIndex(enabled: boolean) {
  return useQuery({ queryKey: docsKeys.index, queryFn: fetchIndex, staleTime: Infinity, enabled });
}

export function useDocsPage(slug: string | null) {
  return useQuery({
    queryKey: docsKeys.page(slug ?? ""),
    queryFn: () => fetchPage(slug ?? ""),
    staleTime: Infinity,
    enabled: slug !== null && slug !== "",
    retry: false,
  });
}

const CSS_ID = "abidata-docs-highlight";

/** Adds the syntax-highlighting stylesheet once. */
export function ensureHighlightCss(): void {
  if (document.getElementById(CSS_ID)) return;
  const link = document.createElement("link");
  link.id = CSS_ID;
  link.rel = "stylesheet";
  link.href = "/api/v1/docs/highlight.css";
  document.head.append(link);
}
