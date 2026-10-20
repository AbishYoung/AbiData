import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import { ConfirmHost } from "@/components/ui";
import type { Me } from "@/lib/session";
import type { MessageThreadDetail, MessageThreadSummary } from "./messagesApi";

const isCompactMock = vi.hoisted(() => vi.fn(() => false));
vi.mock("@/lib/useMediaQuery", () => ({
  useIsCompact: () => isCompactMock(),
  useMediaQuery: () => false,
  COMPACT_QUERY: "(max-width: 767px)",
}));

const navigate = vi.hoisted(() => vi.fn());
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    to,
    params,
    ...rest
  }: {
    children: ReactNode;
    to: string;
    params?: Record<string, string>;
  }) => (
    <a
      href={Object.entries(params ?? {}).reduce(
        (s, [k, v]) => s.replace(`$${k}`, v),
        to,
      )}
      {...rest}
    >
      {children}
    </a>
  ),
  useNavigate: () => navigate,
}));

const api = vi.hoisted(() => ({
  getUnreadMessageCount: vi.fn(),
  listMessageRecipients: vi.fn(),
  listMessageThreads: vi.fn(),
  getMessageThread: vi.fn(),
  createMessageThread: vi.fn(),
  replyMessageThread: vi.fn(),
  patchMessageThread: vi.fn(),
}));
vi.mock("./messagesApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./messagesApi")>()),
  ...api,
}));

import { MessagesPage } from "./MessagesPage";

const me: Me = {
  user: {
    id: "u1",
    email: "priya@example.com",
    username: "priya",
    display_name: "Priya Nair",
    is_platform_admin: false,
  },
  csrf_token: "csrf-1",
  memberships: [
    {
      workspace_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      workspace_name: "Northwind Traders",
      role: "editor",
      status: "active",
    },
  ],
  maps: { tile_url: "", attribution: "" },
};

function makeThreadSummary(
  over: Partial<MessageThreadSummary> = {},
): MessageThreadSummary {
  return {
    id: "thr_01ARZ3NDEKTSV4RRFFQ69G5FAV",
    kind: "direct",
    subject: "Supplier shipment reconciliation",
    created_by: "u2",
    created_at: "2026-10-19T12:00:00.000Z",
    last_message_at: "2026-10-19T12:05:00.000Z",
    message_count: 1,
    unread: false,
    archived: false,
    participants: [
      {
        id: "u1",
        username: "priya",
        display_name: "Priya Nair",
        email: "priya@example.com",
      },
      {
        id: "u2",
        username: "sam",
        display_name: "Sam Rivera",
        email: "sam@example.com",
      },
    ],
    snippet: "Did the Q3 shipment numbers match?",
    last_sender_id: "u2",
    last_sender_username: "sam",
    last_sender_display_name: "Sam Rivera",
    ...over,
  };
}

function makeThreadDetail(
  over: Partial<MessageThreadDetail> = {},
): MessageThreadDetail {
  return {
    ...makeThreadSummary(over),
    messages: [
      {
        id: "msg_01ARZ3NDEKTSV4RRFFQ69G5FAV",
        thread_id: "thr_01ARZ3NDEKTSV4RRFFQ69G5FAV",
        sender_id: "u2",
        sender_username: "sam",
        sender_display_name: "Sam Rivera",
        body: "Did the Q3 shipment numbers match?",
        created_at: "2026-10-19T12:00:00.000Z",
      },
    ],
    ...over,
  };
}

function wrap(node: ReactNode) {
  return render(
    <QueryClientProvider client={queryClient}>
      {node}
      <ConfirmHost />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  queryClient.clear();
  isCompactMock.mockReturnValue(false);
  Object.values(api).forEach((fn) => fn.mockReset());
  navigate.mockReset();
  api.getUnreadMessageCount.mockResolvedValue(0);
  api.listMessageRecipients.mockResolvedValue({
    users: [
      {
        id: "u1",
        username: "priya",
        display_name: "Priya Nair",
        email: "priya@example.com",
      },
      {
        id: "u2",
        username: "sam",
        display_name: "Sam Rivera",
        email: "sam@example.com",
      },
    ],
    workspaces: [
      {
        id: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
        name: "Northwind Traders",
        role: "editor",
      },
    ],
  });
});

afterEach(cleanup);

describe("MessagesPage", () => {
  it("renders threads, displays messages, and sends a reply", async () => {
    const user = userEvent.setup();
    const summary = makeThreadSummary();
    const detail = makeThreadDetail();
    api.listMessageThreads.mockResolvedValue({
      items: [summary],
      unread_count: 0,
    });
    api.getMessageThread.mockResolvedValue(detail);
    api.replyMessageThread.mockResolvedValue({
      ...detail,
      message_count: 2,
      messages: [
        ...(detail.messages ?? []),
        {
          id: "msg_01ARZ3NDEKTSV4RRFFQ69G5FAW",
          thread_id: detail.id,
          parent_id: detail.messages?.[0]?.id ?? "msg_01ARZ3NDEKTSV4RRFFQ69G5FAV",
          sender_id: "u1",
          sender_username: "priya",
          sender_display_name: "Priya Nair",
          body: "All 42 pallets matched!",
          created_at: "2026-10-19T12:10:00.000Z",
        },
      ],
    });

    wrap(<MessagesPage me={me} />);

    expect(
      await screen.findByRole("heading", {
        name: "Supplier shipment reconciliation",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Did the Q3 shipment numbers match?"),
    ).toBeInTheDocument();

    const replyInput = screen.getByLabelText("Reply to thread");
    await user.type(replyInput, "All 42 pallets matched!");
    await user.click(screen.getByRole("button", { name: "Send reply" }));

    await waitFor(() =>
      expect(api.replyMessageThread).toHaveBeenCalledWith(detail.id, {
        body: "All 42 pallets matched!",
      }),
    );
    expect(
      await screen.findByText("All 42 pallets matched!"),
    ).toBeInTheDocument();
  });

  it("composes a direct message to a colleague from the directory", async () => {
    const user = userEvent.setup();
    api.listMessageThreads.mockResolvedValue({ items: [], unread_count: 0 });
    const created = makeThreadDetail({
      subject: "Quick sync on stock levels",
      body: undefined,
    } as never);
    api.createMessageThread.mockResolvedValue(created);
    api.getMessageThread.mockResolvedValue(created);

    wrap(<MessagesPage me={me} />);

    const newButtons = await screen.findAllByRole("button", {
      name: "New message",
    });
    const firstNewBtn = newButtons[0];
    if (!firstNewBtn) throw new Error("expected New message button");
    await user.click(firstNewBtn);

    // Pick Sam Rivera from suggested recipients.
    const samChip = await screen.findByRole("button", { name: /Sam Rivera/ });
    await user.click(samChip);

    await user.type(
      screen.getByLabelText(/^Subject/),
      "Quick sync on stock levels",
    );
    await user.type(
      screen.getByPlaceholderText("Write your message…"),
      "Can we review Warehouse B before noon?",
    );
    await user.click(screen.getByRole("button", { name: "Send message" }));

    await waitFor(() =>
      expect(api.createMessageThread).toHaveBeenCalledWith({
        kind: "direct",
        recipients: ["sam"],
        subject: "Quick sync on stock levels",
        body: "Can we review Warehouse B before noon?",
      }),
    );
  });

  it("defaults to workspace thread when opened inside a workspace", async () => {
    const user = userEvent.setup();
    api.listMessageThreads.mockResolvedValue({ items: [], unread_count: 0 });
    const created = makeThreadDetail({
      kind: "workspace",
      workspace_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
      workspace_name: "Northwind Traders",
      subject: "Schema update tonight",
    });
    api.createMessageThread.mockResolvedValue(created);
    api.getMessageThread.mockResolvedValue(created);

    wrap(
      <MessagesPage
        me={me}
        fixedWorkspaceId="01ARZ3NDEKTSV4RRFFQ69G5FAV"
      />,
    );

    const newButtons = await screen.findAllByRole("button", {
      name: "New message",
    });
    const firstNewBtn = newButtons[0];
    if (!firstNewBtn) throw new Error("expected New message button");
    await user.click(firstNewBtn);

    await user.type(screen.getByLabelText(/^Subject/), "Schema update tonight");
    await user.type(
      screen.getByPlaceholderText("Write your message…"),
      "Adding index on Orders.ShippedDate.",
    );
    await user.click(screen.getByRole("button", { name: "Send message" }));

    await waitFor(() =>
      expect(api.createMessageThread).toHaveBeenCalledWith({
        kind: "workspace",
        workspace_id: "01ARZ3NDEKTSV4RRFFQ69G5FAV",
        subject: "Schema update tonight",
        body: "Adding index on Orders.ShippedDate.",
      }),
    );
  });

  it("targets a specific message with parent_id and allows canceling target", async () => {
    const user = userEvent.setup();
    const summary = makeThreadSummary();
    const detail = makeThreadDetail();
    api.listMessageThreads.mockResolvedValue({
      items: [summary],
      unread_count: 0,
    });
    api.getMessageThread.mockResolvedValue(detail);
    api.replyMessageThread.mockResolvedValue(detail);

    wrap(<MessagesPage me={me} />);

    // Wait for message to load.
    await screen.findByText("Did the Q3 shipment numbers match?");

    // Click "Reply" on message 1.
    const replyBtn = screen.getByRole("button", {
      name: /Reply to message 1 from Sam Rivera/,
    });
    await user.click(replyBtn);

    // Indicator "Replying to Sam Rivera" should appear.
    expect(screen.getByText(/Replying to/)).toBeInTheDocument();

    const replyInput = screen.getByLabelText("Reply to thread");
    await user.type(replyInput, "Specific response to message 1.");
    await user.click(screen.getByRole("button", { name: "Send reply" }));

    await waitFor(() =>
      expect(api.replyMessageThread).toHaveBeenCalledWith(detail.id, {
        body: "Specific response to message 1.",
        parent_id: detail.messages?.[0]?.id,
      }),
    );

    // Now test canceling the reply target.
    await user.click(replyBtn);
    expect(screen.getByText(/Replying to/)).toBeInTheDocument();

    const cancelTargetBtn = screen.getByRole("button", {
      name: "Cancel specific reply target",
    });
    await user.click(cancelTargetBtn);
    expect(screen.queryByText(/Replying to/)).not.toBeInTheDocument();

    await user.type(replyInput, "General reply without parent.");
    await user.click(screen.getByRole("button", { name: "Send reply" }));

    await waitFor(() =>
      expect(api.replyMessageThread).toHaveBeenCalledWith(detail.id, {
        body: "General reply without parent.",
      }),
    );
  });

  it("renders (you) indicator when displaying a note-to-self direct thread", async () => {
    const selfSummary = makeThreadSummary({
      subject: "Personal notes",
      participants: [
        {
          id: "u1",
          username: "priya",
          display_name: "Priya Nair",
          email: "priya@example.com",
        },
      ],
    });
    api.listMessageThreads.mockResolvedValue({
      items: [selfSummary],
      unread_count: 0,
    });
    api.getMessageThread.mockResolvedValue(makeThreadDetail(selfSummary));

    wrap(<MessagesPage me={me} />);

    expect(await screen.findByText(/@priya \(you\)/)).toBeInTheDocument();
  });

  it("smoothly switches between thread list and reader on compact mobile screens", async () => {
    isCompactMock.mockReturnValue(true);
    const user = userEvent.setup();
    const summary = makeThreadSummary();
    const detail = makeThreadDetail();
    api.listMessageThreads.mockResolvedValue({
      items: [summary],
      unread_count: 0,
    });
    api.getMessageThread.mockResolvedValue(detail);

    wrap(<MessagesPage me={me} />);

    // On compact screens, the thread row is visible, but reader is not auto-selected.
    const threadRowBtn = await screen.findByRole("button", {
      name: /Supplier shipment reconciliation/,
    });
    expect(threadRowBtn).toBeInTheDocument();
    expect(screen.queryByText("Back to threads")).not.toBeInTheDocument();

    // Tap thread to open.
    await user.click(threadRowBtn);

    // Reader is now visible with Back button; thread list is hidden.
    const backBtn = await screen.findByRole("button", {
      name: "Back to threads",
    });
    expect(backBtn).toBeInTheDocument();
    expect(
      screen.getByText("Did the Q3 shipment numbers match?"),
    ).toBeInTheDocument();

    // Tap Back to return to thread list.
    await user.click(backBtn);
    expect(
      await screen.findByRole("button", {
        name: /Supplier shipment reconciliation/,
      }),
    ).toBeInTheDocument();
  });
});
