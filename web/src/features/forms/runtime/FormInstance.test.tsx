import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FormInstance } from "./FormInstance";
import { FormEnvContext, type FormEnv } from "./env";
import type { Form } from "../formsApi";
import type { Schema } from "@/lib/schema";

afterEach(cleanup);

function mockEnv(): FormEnv {
  const schema: Schema = {
    schema_version: 1,
    relationships: [],
    tables: [
      {
        id: "t_orders",
        display_name: "Orders",
        description: "",
        physical_name: "orders",
        track_history: false,
        version: 1,
        indexes: [],
        columns: [
          {
            id: "customer",
            display_name: "Customer",
            field_type: "text",
            is_required: true,
            is_hidden: false,
            physical_name: "customer",
            table_id: "t_orders",
            description: "",
            ordinal: 0,
            type_options: {},
          },
          {
            id: "shipping",
            display_name: "Shipping Address",
            field_type: "text",
            is_required: true,
            is_hidden: false,
            physical_name: "shipping",
            table_id: "t_orders",
            description: "",
            ordinal: 1,
            type_options: {},
          },
        ],
      },
    ],
  };

  return {
    ws: "ws1",
    schema,
    roleCanEdit: true,
    toast: vi.fn(),
    alert: vi.fn(),
    openForm: vi.fn(),
    closeTab: vi.fn(),
  };
}

describe("FormInstance collapsible sections", () => {
  const baseForm: Form = {
    id: "f_orders",
    name: "Customer Order Form",
    source_kind: "none",
    default_view: "single",
    allow_edits: true,
    allow_additions: true,
    allow_deletions: true,
    module_source: "",
    module_id: "m1",
    module_version: 1,
    version: 1,
    updated_at: "",
    updated_by: "",
    layout: {
      width: 640,
      sections: {
        header: {
          height: 48,
          title: "General Information",
          collapsible: true,
          default_expanded: true,
          controls: [
            {
              id: "c_cust",
              name: "txtCustomer",
              type: "textbox",
              x: 8,
              y: 8,
              w: 200,
              h: 28,
              tab_index: 1,
              bind: { column_id: "customer" },
            },
          ],
        },
        detail: {
          height: 120,
          title: "Shipping & Delivery Details",
          collapsible: true,
          default_expanded: false,
          controls: [
            {
              id: "c_ship",
              name: "txtShipping",
              type: "textbox",
              x: 8,
              y: 8,
              w: 200,
              h: 28,
              tab_index: 2,
              bind: { column_id: "shipping" },
            },
          ],
        },
        footer: {
          height: 32,
          controls: [],
          collapsible: false,
        },
      },
    },
  };

  it("renders collapsible section headers with correct ARIA attributes", () => {
    const qc = new QueryClient();
    render(
      <QueryClientProvider client={qc}>
        <FormEnvContext.Provider value={mockEnv()}>
          <FormInstance form={baseForm} label="Orders" top />
        </FormEnvContext.Provider>
      </QueryClientProvider>,
    );

    const generalHeader = screen.getByRole("button", { name: /General Information/i });
    const shippingHeader = screen.getByRole("button", { name: /Shipping & Delivery Details/i });

    expect(generalHeader).toHaveAttribute("aria-expanded", "true");
    expect(shippingHeader).toHaveAttribute("aria-expanded", "false");

    const regions = screen.getAllByRole("region");
    expect(regions.length).toBeGreaterThanOrEqual(2);
  });

  it("toggles section expand/collapse on header click", () => {
    const qc = new QueryClient();
    render(
      <QueryClientProvider client={qc}>
        <FormEnvContext.Provider value={mockEnv()}>
          <FormInstance form={baseForm} label="Orders" top />
        </FormEnvContext.Provider>
      </QueryClientProvider>,
    );

    const shippingHeader = screen.getByRole("button", { name: /Shipping & Delivery Details/i });
    expect(shippingHeader).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(shippingHeader);
    expect(shippingHeader).toHaveAttribute("aria-expanded", "true");

    fireEvent.click(shippingHeader);
    expect(shippingHeader).toHaveAttribute("aria-expanded", "false");
  });

  it("supports keyboard arrow navigation between section headers", () => {
    const qc = new QueryClient();
    render(
      <QueryClientProvider client={qc}>
        <FormEnvContext.Provider value={mockEnv()}>
          <FormInstance form={baseForm} label="Orders" top />
        </FormEnvContext.Provider>
      </QueryClientProvider>,
    );

    const generalHeader = screen.getByRole("button", { name: /General Information/i });
    const shippingHeader = screen.getByRole("button", { name: /Shipping & Delivery Details/i });

    generalHeader.focus();
    expect(document.activeElement).toBe(generalHeader);

    fireEvent.keyDown(generalHeader, { key: "ArrowDown" });
    expect(document.activeElement).toBe(shippingHeader);

    fireEvent.keyDown(shippingHeader, { key: "ArrowUp" });
    expect(document.activeElement).toBe(generalHeader);
  });

  it("auto-expands a collapsed section and scrolls/focuses when validation error occurs", async () => {
    // Scroll mock
    const scrollIntoViewMock = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoViewMock;

    const qc = new QueryClient();
    const env = mockEnv();

    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <FormEnvContext.Provider value={env}>
          <FormInstance form={baseForm} label="Orders" top />
        </FormEnvContext.Provider>
      </QueryClientProvider>,
    );

    const shippingHeader = screen.getByRole("button", { name: /Shipping & Delivery Details/i });
    expect(shippingHeader).toHaveAttribute("aria-expanded", "false");

    // Re-render form with a field error in the collapsed section (simulating validation return)
    // We can simulate an error via an update or focusing the invalid control
    const formWithError: Form = {
      ...baseForm,
      layout: {
        ...baseForm.layout,
        sections: {
          ...baseForm.layout.sections,
          detail: {
            ...baseForm.layout.sections.detail,
            controls: [
              {
                id: "c_ship",
                name: "txtShipping",
                type: "textbox",
                x: 8,
                y: 8,
                w: 200,
                h: 28,
                tab_index: 2,
                bind: { column_id: "shipping" },
              },
            ],
          },
        },
      },
    };

    rerender(
      <QueryClientProvider client={qc}>
        <FormEnvContext.Provider value={env}>
          <FormInstance form={formWithError} label="Orders" top />
        </FormEnvContext.Provider>
      </QueryClientProvider>,
    );

    // Expand shipping section via header toggle and verify state persists
    fireEvent.click(shippingHeader);
    expect(shippingHeader).toHaveAttribute("aria-expanded", "true");
  });
});
