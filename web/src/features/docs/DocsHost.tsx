import { useState } from "react";
import { suspended } from "@/lib/suspended";
import { useDocs } from "./docsStore";

// The documentation panel (its search index and pages) loads the first time it is opened
// (ADR 0015, review R7) and then stays mounted.
const DocsPanel = suspended<object>(() => import("./DocsPanel").then((m) => ({ default: m.DocsPanel })));

/** Mount once near the app root, in place of DocsPanel. */
export function DocsHost() {
  const open = useDocs((s) => s.open);
  const [wanted, setWanted] = useState(open);
  if (open && !wanted) setWanted(true);
  return wanted ? <DocsPanel /> : null;
}
