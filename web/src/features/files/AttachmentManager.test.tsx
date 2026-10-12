import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ApiError } from "@/lib/api";
import { AttachmentManager } from "./AttachmentManager";
import type { FileMeta } from "./filesApi";

const metas: Record<string, FileMeta> = Object.fromEntries(
  ["a", "b", "c"].map((id) => [
    id,
    { id, name: `${id}.png`, size: 2048, content_type: "image/png", sha256: id, state: "attached", has_thumbnail: true, created_at: "2026-01-01T00:00:00Z" } satisfies FileMeta,
  ]),
);

interface Pending {
  file: File;
  resolve: (m: FileMeta) => void;
  reject: (e: unknown) => void;
}
const uploads: Pending[] = [];

vi.mock("./filesApi", async () => {
  const actual = await vi.importActual<typeof import("./filesApi")>("./filesApi");
  return {
    ...actual,
    useFileInfos: (_ws: string, ids: readonly string[]) => ids.map((id) => ({ id, meta: metas[id] ?? null, loading: false, failed: false })),
    seedFileMeta: (_qc: unknown, _ws: string, meta: FileMeta) => {
      metas[meta.id] = meta;
    },
    uploadFile: (_ws: string, file: File) => {
      let resolve!: (m: FileMeta) => void;
      let reject!: (e: unknown) => void;
      const promise = new Promise<FileMeta>((res, rej) => {
        resolve = res;
        reject = rej;
      });
      uploads.push({ file, resolve, reject });
      return { promise, abort: () => reject(new ApiError({ status: 0, code: "INTERNAL", title: "x", message: "x", canceled: true })) };
    },
  };
});

function meta(id: string, name: string): FileMeta {
  return { id, name, size: 10, content_type: "image/png", sha256: id, state: "pending", has_thumbnail: true, created_at: "2026-01-01T00:00:00Z" };
}

function Harness({ initial, max = 5, readOnly = false, onValue }: { initial: string[]; max?: number; readOnly?: boolean; onValue?: (v: string[]) => void }) {
  const [value, setValue] = useState(initial);
  return (
    <QueryClientProvider client={new QueryClient()}>
      <AttachmentManager
        ws="w1"
        label="Photos"
        value={value}
        maxFiles={max}
        readOnly={readOnly}
        onChange={(v) => {
          setValue(v);
          onValue?.(v);
        }}
      />
      <output data-testid="value">{value.join(",")}</output>
    </QueryClientProvider>
  );
}

const names = () => screen.getAllByTestId("attachment-tile").map((t) => within(t).getByText(/\.png$/).textContent);
const png = (name: string) => new File([new Uint8Array([1, 2, 3])], name, { type: "image/png" });

beforeEach(() => {
  uploads.length = 0;
});
afterEach(cleanup);

describe("AttachmentManager: reorder and remove", () => {
  it("moves files with the keyboard-accessible buttons and stops at the ends", async () => {
    const user = userEvent.setup();
    render(<Harness initial={["a", "b", "c"]} />);
    expect(names()).toEqual(["a.png", "b.png", "c.png"]);
    expect(screen.getByRole("button", { name: "Move a.png earlier" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Move c.png later" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Move c.png earlier" }));
    expect(names()).toEqual(["a.png", "c.png", "b.png"]);
    await user.click(screen.getByRole("button", { name: "Move a.png later" }));
    expect(names()).toEqual(["c.png", "a.png", "b.png"]);
  });

  it("removes a file from the value", async () => {
    const user = userEvent.setup();
    const seen: string[][] = [];
    render(<Harness initial={["a", "b"]} onValue={(v) => seen.push(v)} />);
    await user.click(screen.getByRole("button", { name: "Remove a.png" }));
    expect(seen).toEqual([["b"]]);
    expect(screen.getByTestId("attachment-count")).toHaveTextContent("1 of 5 files");
    await user.click(screen.getByRole("button", { name: "Remove b.png" }));
    expect(screen.getByTestId("attachment-empty")).toBeInTheDocument();
  });

  it("offers no editing controls when read-only", () => {
    render(<Harness initial={["a", "b"]} readOnly />);
    expect(screen.queryByRole("button", { name: /^Remove/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /^Move/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Browse" })).toBeNull();
    expect(screen.getByRole("link", { name: "Download a.png" })).toHaveAttribute("href", expect.stringContaining("/files/a/content?download=true"));
  });
});

describe("AttachmentManager: uploads and limits", () => {
  it("adds files in the order they were chosen, however the uploads finish", async () => {
    const user = userEvent.setup();
    render(<Harness initial={[]} />);
    await user.upload(screen.getByTestId("attachment-input"), [png("one.png"), png("two.png"), png("three.png")]);
    expect(uploads).toHaveLength(3);
    expect(screen.getAllByTestId("upload-row")).toHaveLength(3);
    await act(async () => {
      uploads[2]?.resolve(meta("n3", "three.png"));
      uploads[0]?.resolve(meta("n1", "one.png"));
    });
    // The second is still uploading: the third waits for it.
    expect(screen.getByTestId("value")).toHaveTextContent("n1");
    await act(async () => {
      uploads[1]?.resolve(meta("n2", "two.png"));
    });
    expect(screen.getByTestId("value")).toHaveTextContent("n1,n2,n3");
    expect(screen.queryAllByTestId("upload-row")).toHaveLength(0);
  });

  it("takes only as many files as fit and says so", async () => {
    const user = userEvent.setup();
    render(<Harness initial={["a"]} max={3} />);
    await user.upload(screen.getByTestId("attachment-input"), [png("1.png"), png("2.png"), png("3.png"), png("4.png")]);
    expect(uploads).toHaveLength(2);
    expect(screen.getByText(/Only 2 of the 4 files added: this field holds at most 3 files/)).toBeInTheDocument();
    // While two are uploading the field counts them: it is full.
    expect(screen.getByRole("button", { name: "Browse" })).toBeDisabled();
  });

  it("disables adding at the limit and a single-document field says 1 file", () => {
    render(<Harness initial={["a"]} max={1} />);
    expect(screen.getByRole("button", { name: "Browse" })).toBeDisabled();
    expect(screen.getByText("Limit reached: 1 file.")).toBeInTheDocument();
    expect(screen.getByTestId("attachment-count")).toHaveTextContent("1 of 1 file");
  });

  it("shows why an upload failed and lets the user dismiss it", async () => {
    const user = userEvent.setup();
    render(<Harness initial={[]} />);
    await user.upload(screen.getByTestId("attachment-input"), png("big.png"));
    await act(async () => {
      uploads[0]?.reject(new ApiError({ status: 507, code: "QUOTA_EXCEEDED", title: "Over quota", message: "This workspace has used all of its file storage." }));
    });
    expect(screen.getByRole("alert")).toHaveTextContent("big.png: This workspace has used all of its file storage.");
    expect(screen.getByTestId("value")).toHaveTextContent("");
    await user.click(screen.getByRole("button", { name: "Dismiss the error for big.png" }));
    expect(screen.queryByTestId("upload-row")).toBeNull();
  });

  it("ignores a drop that carries no files, and drops files into the zone", () => {
    render(<Harness initial={[]} />);
    const zone = screen.getByTestId("attachment-dropzone");
    fireEvent.drop(zone, { dataTransfer: { types: ["text/plain"], files: [] } });
    expect(uploads).toHaveLength(0);
    fireEvent.drop(zone, { dataTransfer: { types: ["Files"], files: [png("dropped.png")] } });
    expect(uploads).toHaveLength(1);
  });
});
