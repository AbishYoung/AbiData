import { useQuery } from "@tanstack/react-query";
import type { components } from "@/generated/api";
import { api } from "./api";
import { keys } from "./events";

export type Schema = components["schemas"]["Schema"];
export type Table = components["schemas"]["Table"];
export type Column = components["schemas"]["Column"];
export type FieldType = Column["field_type"];

/** The workspace catalog; refreshed by schema.changed events. */
export function useSchema(ws: string) {
  return useQuery({
    queryKey: keys.schema(ws),
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}/schema", { ...init, params: { path: { ws } } }), {
        label: "Loading tables",
      }),
  });
}

export function findTable(schema: Schema | undefined, id: string): Table | undefined {
  return schema?.tables?.find((t) => t.id === id);
}
