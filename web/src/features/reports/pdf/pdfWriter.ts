/**
 * A small vector PDF 1.4 writer: pages of filled rectangles, rules and text in the base-14 fonts
 * (no embedded fonts, images or compression), enough for banded reports. Coordinates are CSS px
 * (96 dpi) with the origin at the top-left of the page, as the paginator uses them.
 */

export type Rgb = readonly [number, number, number];

/** Hard limits so a runaway report cannot build an unbounded document in memory. */
export const MAX_PDF_PAGES = 2000;
export const MAX_PDF_BYTES = 256 << 20;

const PT = 0.75; // 1 px in points

const WIN_ANSI: Readonly<Record<number, number>> = {
  0x20ac: 0x80,
  0x201a: 0x82,
  0x0192: 0x83,
  0x201e: 0x84,
  0x2026: 0x85,
  0x2020: 0x86,
  0x2021: 0x87,
  0x02c6: 0x88,
  0x2030: 0x89,
  0x0160: 0x8a,
  0x2039: 0x8b,
  0x0152: 0x8c,
  0x017d: 0x8e,
  0x2018: 0x91,
  0x2019: 0x92,
  0x201c: 0x93,
  0x201d: 0x94,
  0x2022: 0x95,
  0x2013: 0x96,
  0x2014: 0x97,
  0x02dc: 0x98,
  0x2122: 0x99,
  0x0161: 0x9a,
  0x203a: 0x9b,
  0x0153: 0x9c,
  0x017e: 0x9e,
  0x0178: 0x9f,
};

/** Text as a PDF literal string in WinAnsi; characters outside it become "?". */
export function pdfString(text: string): string {
  let out = "(";
  for (const ch of text) {
    const cp = ch.codePointAt(0) ?? 63;
    let b = cp;
    if (cp >= 0x80) {
      const mapped = WIN_ANSI[cp];
      b = mapped ?? (cp <= 0xff && (cp < 0x80 || cp > 0x9f) ? cp : 63);
    }
    if (b < 32 && b !== 9) b = 32;
    if (b === 0x28 || b === 0x29 || b === 0x5c) out += "\\" + String.fromCharCode(b);
    else out += String.fromCharCode(b);
  }
  return out + ")";
}

function num(n: number): string {
  const r = Math.round(n * 1000) / 1000;
  return Object.is(r, -0) ? "0" : String(r);
}

function rgb(c: Rgb): string {
  return `${num(c[0])} ${num(c[1])} ${num(c[2])}`;
}

/** One page being drawn. */
export class PdfPage {
  private readonly ops: string[] = [];
  constructor(
    readonly widthPx: number,
    readonly heightPx: number,
    private readonly fontRef: (name: string) => string,
  ) {}

  private y(py: number): number {
    return (this.heightPx - py) * PT;
  }

  save(): void {
    this.ops.push("q");
  }
  restore(): void {
    this.ops.push("Q");
  }
  clip(x: number, y: number, w: number, h: number): void {
    this.ops.push(`${num(x * PT)} ${num(this.y(y + h))} ${num(w * PT)} ${num(h * PT)} re W n`);
  }
  fillRect(x: number, y: number, w: number, h: number, color: Rgb): void {
    if (w <= 0 || h <= 0) return;
    this.ops.push(`${rgb(color)} rg ${num(x * PT)} ${num(this.y(y + h))} ${num(w * PT)} ${num(h * PT)} re f`);
  }
  /** A straight rule of the given width (px); dash is an on/off pattern in px. */
  line(x1: number, y1: number, x2: number, y2: number, width: number, color: Rgb, dash?: readonly [number, number]): void {
    const d = dash ? `[${num(dash[0] * PT)} ${num(dash[1] * PT)}] 0 d` : "[] 0 d";
    this.ops.push(`${rgb(color)} RG ${num(width * PT)} w ${d} ${num(x1 * PT)} ${num(this.y(y1))} m ${num(x2 * PT)} ${num(this.y(y2))} l S`);
  }
  strokeRect(x: number, y: number, w: number, h: number, width: number, color: Rgb): void {
    this.ops.push(`${rgb(color)} RG ${num(width * PT)} w [] 0 d ${num((x + width / 2) * PT)} ${num(this.y(y + h - width / 2))} ${num((w - width) * PT)} ${num((h - width) * PT)} re S`);
  }
  /** Draws one line of text with its baseline at (x, baseline). */
  text(x: number, baseline: number, str: string, font: string, sizePx: number, color: Rgb): void {
    if (str === "") return;
    this.ops.push(`BT ${rgb(color)} rg /${this.fontRef(font)} ${num(sizePx * PT)} Tf ${num(x * PT)} ${num(this.y(baseline))} Td ${pdfString(str)} Tj ET`);
  }
  content(): string {
    return this.ops.join("\n");
  }
}

/** A document under construction. */
export class PdfDoc {
  private readonly pages: PdfPage[] = [];
  private readonly fonts = new Map<string, string>(); // base font → resource name
  title = "";

  addPage(widthPx: number, heightPx: number): PdfPage {
    if (this.pages.length >= MAX_PDF_PAGES) throw new Error(`The PDF would have more than ${MAX_PDF_PAGES} pages.`);
    const page = new PdfPage(widthPx, heightPx, (name) => this.fontName(name));
    this.pages.push(page);
    return page;
  }

  private fontName(base: string): string {
    let n = this.fonts.get(base);
    if (!n) {
      n = `F${this.fonts.size + 1}`;
      this.fonts.set(base, n);
    }
    return n;
  }

  /** Serializes the document (PDF 1.4, latin-1 bytes). */
  build(): Uint8Array<ArrayBuffer> {
    const objs: string[] = [];
    const add = (body: string): number => objs.push(body);
    // 1 catalog, 2 pages tree, then fonts, then (page, content) pairs.
    add("<< /Type /Catalog /Pages 2 0 R >>");
    add(""); // pages tree, filled below
    const fontIds = new Map<string, number>();
    const fontRes: string[] = [];
    for (const [base, name] of this.fonts) {
      const id = add(`<< /Type /Font /Subtype /Type1 /BaseFont /${base} /Encoding /WinAnsiEncoding >>`);
      fontIds.set(name, id);
      fontRes.push(`/${name} ${id} 0 R`);
    }
    const resources = `<< /Font << ${fontRes.join(" ")} >> >>`;
    const kids: number[] = [];
    for (const p of this.pages) {
      const content = p.content();
      const cid = add(`<< /Length ${content.length} >>\nstream\n${content}\nendstream`);
      const pid = add(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 ${num(p.widthPx * PT)} ${num(p.heightPx * PT)}] /Resources ${resources} /Contents ${cid} 0 R >>`);
      kids.push(pid);
    }
    objs[1] = `<< /Type /Pages /Count ${kids.length} /Kids [${kids.map((k) => `${k} 0 R`).join(" ")}] >>`;
    const infoId = add(`<< /Title ${pdfString(this.title)} /Producer (AbiData) >>`);

    let out = "%PDF-1.4\n%âãÏÓ\n";
    const offsets: number[] = [];
    objs.forEach((body, i) => {
      offsets.push(out.length);
      out += `${i + 1} 0 obj\n${body}\nendobj\n`;
      if (out.length > MAX_PDF_BYTES) throw new Error("The PDF is too large.");
    });
    const xref = out.length;
    out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n`;
    for (const o of offsets) out += `${String(o).padStart(10, "0")} 00000 n \n`;
    out += `trailer\n<< /Size ${objs.length + 1} /Root 1 0 R /Info ${infoId} 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
    const bytes = new Uint8Array(new ArrayBuffer(out.length));
    for (let i = 0; i < out.length; i++) bytes[i] = out.charCodeAt(i) & 0xff;
    return bytes;
  }
}
