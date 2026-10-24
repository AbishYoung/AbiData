import { useQuery } from "@tanstack/react-query";
import { Database } from "lucide-react";
import { badge, warningBox } from "@/components/styles";
import { workspaceQuery } from "@/features/workspace/workspaceQuery";

/** The SQL dialect of the install's storage backend (ADR 0012 §4). */
export type SqlDialect = "sqlite" | "postgres";

const labels: Record<SqlDialect, string> = { sqlite: "SQLite", postgres: "PostgreSQL" };

export function dialectLabel(d: string | undefined): string {
  return d === "sqlite" || d === "postgres" ? labels[d] : (d ?? "");
}

/** The workspace's SQL dialect, once the workspace summary has loaded. */
export function useSqlDialect(ws: string): SqlDialect | undefined {
  return useQuery(workspaceQuery(ws)).data?.sql_dialect;
}

/** A toolbar badge naming the dialect SQL is written in here. */
export function DialectBadge({ dialect }: { dialect: SqlDialect | undefined }) {
  if (!dialect) return null;
  return (
    <span className={`${badge} gap-1`} data-testid="sql-dialect">
      <Database size={12} aria-hidden="true" />
      {labels[dialect]} SQL
    </span>
  );
}

/**
 * Warns when a saved query was written in another dialect than the install's
 * (for example after abidata migrate-backend), so its SQL may need changes.
 */
export function DialectMismatch({ dialect, saved }: { dialect: SqlDialect | undefined; saved: string | undefined }) {
  if (!dialect || !saved || saved === dialect) return null;
  return (
    <p role="status" className={`${warningBox} m-0`}>
      This query was written in {dialectLabel(saved)} SQL, but this install runs {labels[dialect]}. Check that it still
      runs, and update it if it does not.
    </p>
  );
}
