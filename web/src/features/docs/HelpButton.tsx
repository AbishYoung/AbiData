import { CircleHelp } from "lucide-react";
import { btn } from "@/components/styles";
import { ariaKeyShortcuts, shortcutKeys } from "@/lib/shortcuts";
import { openDocs } from "./docsStore";

/** The "?" button in the app shell that opens the documentation panel. */
export function HelpButton() {
  return (
    <button
      type="button"
      className={`${btn} !px-2`}
      aria-label="Open documentation"
      aria-keyshortcuts={ariaKeyShortcuts(shortcutKeys("docs"))}
      title="Documentation (?)"
      onClick={() => openDocs()}
    >
      <CircleHelp size={16} aria-hidden="true" />
    </button>
  );
}
