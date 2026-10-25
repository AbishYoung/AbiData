import { describe, expect, it } from "vitest";
import { OBJECT_KINDS } from "@/components/objectKinds";
import { btn, btnPrimary, card, dialogFooter, dialogHeader, dialogOverlay, dialogPanel, menuItem, menuItemDanger, menuItemFor, segActive, segInactive, segTray } from "@/components/styles";

// The tactile controls of ADR 0021: shapes only; colours come from tokens (the token lint covers raw colours).
describe("tactile control styles", () => {
  it("gives buttons a gradient face and tactile press feedback", () => {
    expect(btnPrimary).toContain("bg-gradient-to-b");
    expect(btnPrimary).toContain("active:scale-[0.98]");
    expect(btn).toContain("active:scale-[0.98]");
    expect(btn).toContain("specular-sm");
  });

  it("raises cards with the specular edge", () => {
    expect(card).toContain("specular-sm");
  });

  it("builds the segmented control from a recessed tray and a floating active card", () => {
    expect(segTray).toContain("bg-surface-3/60");
    expect(segActive).toContain("bg-surface");
    expect(segActive).toContain("shadow-xs");
    expect(segInactive).toContain("border-transparent");
  });

  it("styles every object kind as a satin jewel with a tinted border", () => {
    for (const k of ["table", "query", "form", "report", "script", "job"] as const) {
      const soft = OBJECT_KINDS[k].soft;
      expect(soft).toContain(`border-kind-${k}/25`);
      expect(soft).toContain(`from-kind-${k}-soft/90`);
      expect(soft).toContain(`to-kind-${k}-soft/40`);
    }
  });
});

describe("overlay styles", () => {
  it("gives dialogs a surface-2 header band, a footer bar and a blurred scrim", () => {
    expect(dialogHeader).toContain("bg-surface-2");
    expect(dialogHeader).toContain("border-b");
    expect(dialogFooter).toContain("justify-end");
    expect(dialogPanel).toContain("specular-lg");
    expect(dialogOverlay).toContain("backdrop-blur");
  });

  it("styles destructive menu items in danger", () => {
    expect(menuItemFor("delete")).toBe(menuItemDanger);
    expect(menuItemFor("rename")).toBe(menuItem);
    expect(menuItemDanger).toContain("text-danger");
  });
});
