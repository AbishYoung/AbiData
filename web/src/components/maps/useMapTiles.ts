import { useMe } from "@/lib/session";

export interface MapTiles {
  tile_url: string;
  attribution: string;
}

/** The administrator's base map tiles, or null when tiles are off (the default). */
export function useMapTiles(): MapTiles | null {
  const me = useMe();
  const maps = me.data?.maps;
  return maps && maps.tile_url ? maps : null;
}
