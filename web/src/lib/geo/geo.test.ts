import { describe, expect, it } from "vitest";
import { addHole, addPart, addVertex, areaFromDraft, draftFromArea, moveVertex, removeVertex, EMPTY_DRAFT } from "./areaDraft";
import { areaText, formatArea, formatGeoValue, formatMetres, formatPoint } from "./format";
import { areaM2, bboxOf, parseAreaText, parseGeometry, parseLatLonText, pointGeometry, vertexCount, type AreaGeometry } from "./geometry";
import { fitProjection, formatDegree, graticule, latToTileY, lonToTileX, niceStep, tileLat, tileLon, tileUrl, tileZoom } from "./projection";

const square: AreaGeometry = { type: "Polygon", coordinates: [[[0, 0], [0.01, 0], [0.01, 0.01], [0, 0.01], [0, 0]]] };

describe("format", () => {
  it("formats points with 5 decimals, latitude first", () => {
    expect(formatPoint(51.5013512, -0.1418923)).toBe("51.50135, -0.14189");
    expect(formatPoint(-0.000001, 0)).toBe("0.00000, 0.00000");
    expect(formatGeoValue({ type: "Point", coordinates: [-0.1418923, 51.5013512] })).toBe("51.50135, -0.14189");
    expect(formatGeoValue('{"type":"Point","coordinates":[2.5,-1.25]}')).toBe("-1.25000, 2.50000");
  });

  it("formats area sizes like the report server", () => {
    expect(areaText(0)).toBe("0 m²");
    expect(areaText(850.4)).toBe("850 m²");
    expect(areaText(12345.6)).toBe("12,346 m²");
    expect(areaText(3_400_000)).toBe("3.4 km²");
    expect(areaText(160_000)).toBe("0.2 km²");
    expect(areaText(12.34e7)).toBe("123 km²");
    expect(areaText(1.5e10)).toBe("15,000 km²");
    expect(formatArea(square)).toBe("Area · 4 points · 1.2 km²");
  });

  it("never shows [object Object]", () => {
    expect(formatGeoValue({ type: "Thing" })).toBe('{"type":"Thing"}');
    expect(formatGeoValue(null)).toBe("");
    expect(formatMetres(12.4)).toBe("12 m");
    expect(formatMetres(1520)).toBe("1.5 km");
  });
});

describe("geometry", () => {
  it("computes the spherical-excess area (matches the server)", () => {
    // 0.01° square at the equator: (0.01·π/180)·R² · sin(0.01°) ≈ 1.2364 km².
    expect(areaM2(square) / 1e6).toBeCloseTo(1.2364, 3);
    const holed: AreaGeometry = {
      type: "Polygon",
      coordinates: [square.coordinates[0] ?? [], [[0.004, 0.004], [0.004, 0.006], [0.006, 0.006], [0.006, 0.004], [0.004, 0.004]]],
    };
    expect(areaM2(holed)).toBeCloseTo(areaM2(square) * 0.96, -2);
    expect(vertexCount(holed)).toBe(8);
  });

  it("parses values, Features and text", () => {
    expect(parseGeometry({ type: "Feature", geometry: { type: "Point", coordinates: [1, 2] } })).toEqual({ type: "Point", coordinates: [1, 2] });
    expect(parseGeometry("nope")).toBeNull();
    expect(parseGeometry({ type: "Polygon", coordinates: [[[0, 0], ["x", 1]]] })).toBeNull();
    expect(parseLatLonText("51.5, -0.1")).toEqual({ ok: true, value: { lat: 51.5, lon: -0.1 } });
    expect(parseLatLonText("91, 0").ok).toBe(false);
    expect(parseLatLonText("")).toEqual({ ok: true, value: null });
    expect(pointGeometry(51.501351234, -0.14189234)).toEqual({ type: "Point", coordinates: [-0.1418923, 51.5013512] });
  });

  it("parses pasted areas", () => {
    expect(parseAreaText(JSON.stringify(square))).toEqual({ ok: true, value: square });
    const fc = { type: "FeatureCollection", features: [{ type: "Feature", geometry: square }, { type: "Feature", geometry: square }] };
    const r = parseAreaText(JSON.stringify(fc));
    expect(r.ok && r.value.type).toBe("MultiPolygon");
    expect(parseAreaText("{").ok).toBe(false);
    expect(parseAreaText('{"type":"Point","coordinates":[0,0]}').ok).toBe(false);
    expect(parseAreaText('{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}').ok).toBe(false);
  });

  it("finds bounds", () => {
    expect(bboxOf([square, { type: "Point", coordinates: [-1, 5] }, null])).toEqual([-1, 0, 0.01, 5]);
    expect(bboxOf([])).toBeNull();
  });
});

describe("area draft", () => {
  it("adds, moves and removes vertices, with holes and parts", () => {
    let d = EMPTY_DRAFT;
    d = addVertex(d, 0, 0);
    d = addVertex(d, 1, 0);
    expect(areaFromDraft(d).area).toBeNull();
    expect(areaFromDraft(d).problem).toMatch(/3 corners/);
    d = addVertex(d, 1, 1);
    let res = areaFromDraft(d);
    expect(res.area).toEqual({ type: "Polygon", coordinates: [[[0, 0], [1, 0], [1, 1], [0, 0]]] });
    d = moveVertex(d, { p: 0, r: 0, v: 2 }, 0, 1);
    d = addHole(d);
    expect(d.active).toEqual({ p: 0, r: 1 });
    d = addVertex(addVertex(d, 0.1, 0.1), 0.2, 0.1);
    res = areaFromDraft(d);
    expect(res.problem).toMatch(/left out/);
    expect(res.area?.type).toBe("Polygon");
    d = addPart(d);
    d = addVertex(addVertex(addVertex(d, 5, 5), 6, 5), 6, 6);
    expect(areaFromDraft(d).area?.type).toBe("MultiPolygon");
    d = removeVertex(d, { p: 1, r: 0, v: 2 });
    expect(d.selected).toEqual({ p: 1, r: 0, v: 1 });
    expect(areaFromDraft(d).area?.type).toBe("Polygon");
  });

  it("inserts after the selected vertex and round-trips an area", () => {
    const d = draftFromArea(square);
    expect(d.polygons[0]?.[0]).toHaveLength(4);
    const ins = addVertex({ ...d, selected: { p: 0, r: 0, v: 0 } }, 0.005, -0.001);
    expect(ins.polygons[0]?.[0]?.[1]).toEqual([0.005, -0.001]);
    expect(areaFromDraft(draftFromArea(square)).area).toEqual(square);
  });
});

describe("projection", () => {
  it("fits bounds with an equirectangular projection scaled by cos(lat)", () => {
    const p = fitProjection([0, 60, 2, 61], 400, 200, 0);
    // At 60.5°N a degree of longitude is about half a degree of latitude: width-bound fit.
    const cos = Math.cos((60.5 * Math.PI) / 180);
    expect(p.scale).toBeCloseTo(Math.min(400 / (2 * cos), 200 / 1), 6);
    const [x0, y0] = p.project(0, 61);
    const [x1, y1] = p.project(2, 60);
    expect(x1 - x0).toBeCloseTo(2 * cos * p.scale, 6);
    expect(y1 - y0).toBeCloseTo(p.scale, 6);
    expect((x0 + x1) / 2).toBeCloseTo(200, 6);
    const [lon, lat] = p.invert(...p.project(1.5, 60.25));
    expect(lon).toBeCloseTo(1.5, 9);
    expect(lat).toBeCloseTo(60.25, 9);
  });

  it("gives single points a visible extent", () => {
    const p = fitProjection([10, 10, 10, 10], 100, 100, 0);
    expect(p.view[2] - p.view[0]).toBeGreaterThan(0.009);
    expect(p.project(10, 10)).toEqual([50, 50]);
  });

  it("picks round graticule steps and labels", () => {
    expect(niceStep(1)).toBe(0.2);
    expect(niceStep(0.03)).toBe(0.01);
    expect(niceStep(400)).toBe(90);
    const g = graticule([-0.15, 51.48, -0.1, 51.52]);
    expect(g.step).toBe(0.01);
    expect(g.lats).toEqual([51.48, 51.49, 51.5, 51.51, 51.52]);
    expect(formatDegree(51.5, "lat", 0.5)).toBe("51.5°N");
    expect(formatDegree(-0.25, "lon", 0.25)).toBe("0.25°W");
    expect(formatDegree(0, "lon", 1)).toBe("0°");
  });

  it("maps tiles", () => {
    expect(lonToTileX(tileLon(5, 4), 4)).toBe(5);
    expect(latToTileY(tileLat(7, 4) - 0.001, 4)).toBe(7);
    expect(tileUrl("https://{s}.t.org/{z}/{x}/{y}{r}.png", 2, 5, 1)).toBe("https://c.t.org/2/1/1.png");
    expect(tileZoom(fitProjection([0, 0, 360 / 16, 1], 256 * 16, 100000, 0))).toBe(8);
  });
});
