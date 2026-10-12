import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { AttachmentThumbs, FileNames, FilesWorkspaceProvider, useFilesWorkspace } from "./FileParts";
import type { FileInfo, FileMeta } from "./filesApi";

const png: FileMeta = { id: "p1", name: "sighting.png", size: 100, content_type: "image/png", sha256: "x", state: "attached", has_thumbnail: true, created_at: "2026-01-01T00:00:00Z" };
const pdf: FileMeta = { id: "d1", name: "permit.pdf", size: 100, content_type: "application/pdf", sha256: "y", state: "attached", has_thumbnail: false, created_at: "2026-01-01T00:00:00Z" };

let lookup: Record<string, FileMeta | null | undefined> = {};
vi.mock("./filesApi", async () => {
  const actual = await vi.importActual<typeof import("./filesApi")>("./filesApi");
  return {
    ...actual,
    useFileInfos: (_ws: string, ids: readonly string[]): FileInfo[] =>
      ids.map((id) => ({ id, meta: lookup[id], loading: lookup[id] === undefined, failed: false })),
  };
});

afterEach(() => {
  cleanup();
  lookup = {};
});

describe("FileNames (row history)", () => {
  it("lists names and says 'file removed' for a purged file", () => {
    lookup = { p1: png, gone: null, d1: pdf };
    render(<FileNames ws="w" ids={["p1", "gone", "d1"]} />);
    expect(screen.getByTestId("file-names")).toHaveTextContent("sighting.png, file removed, permit.pdf");
    expect(screen.getByText("file removed").tagName).toBe("EM");
  });
  it("never prints a raw id while loading", () => {
    lookup = {};
    render(<FileNames ws="w" ids={["01HZZZZZZZZZZZZZZZZZZZZZZZ"]} />);
    expect(screen.getByTestId("file-names")).not.toHaveTextContent("01HZ");
  });
});

describe("AttachmentThumbs (datasheet cell)", () => {
  it("shows the count, thumbnails of at most three files and the rest as +n", () => {
    lookup = { p1: png, p2: { ...png, id: "p2" }, p3: { ...png, id: "p3" }, p4: { ...png, id: "p4" } };
    const { container } = render(<AttachmentThumbs ws="w1" ids={["p1", "p2", "p3", "p4"]} />);
    expect(screen.getByText("4 files")).toBeInTheDocument();
    const imgs = container.querySelectorAll("img");
    expect(imgs).toHaveLength(3);
    expect(imgs[0]?.getAttribute("src")).toBe("/api/v1/w/w1/files/p1/thumbnail");
    expect(screen.getByText("+1")).toBeInTheDocument();
  });
  it("uses an icon, not a broken image, for a file without a thumbnail", () => {
    lookup = { d1: pdf };
    const { container } = render(<AttachmentThumbs ws="w1" ids={["d1"]} />);
    expect(container.querySelectorAll("img")).toHaveLength(0);
    expect(container.querySelectorAll("svg").length).toBeGreaterThan(0);
  });
  it("renders nothing for an empty value", () => {
    const { container } = render(<AttachmentThumbs ws="w1" ids={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("useFilesWorkspace", () => {
  function Show() {
    return <span>{useFilesWorkspace()}</span>;
  }
  it("reads the provided workspace", () => {
    render(
      <FilesWorkspaceProvider ws="abc">
        <Show />
      </FilesWorkspaceProvider>,
    );
    expect(screen.getByText("abc")).toBeInTheDocument();
  });
  it("fails loudly without a provider", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => undefined);
    expect(() => render(<Show />)).toThrow(/FilesWorkspaceProvider/);
    spy.mockRestore();
  });
});
