import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import "./styles/tokens.css";
import "./styles/chrome.css";
import { queryClient } from "./app/queryClient";
import { router } from "./app/router";
import { ErrorHost } from "./components/ErrorDialog";
import { ActivityBanner } from "./components/ActivityBanner";
import { DocsPanel } from "./features/docs/DocsPanel";
import { CommandPalette } from "./features/palette/CommandPalette";
import { GlobalShortcuts } from "./features/palette/GlobalShortcuts";
import { applyTheme, loadThemePref } from "./lib/theme";

applyTheme(loadThemePref());

const rootElement = document.getElementById("root");
if (!rootElement) throw new Error("Missing #root element");

createRoot(rootElement).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
      <ErrorHost />
      <ActivityBanner />
      <CommandPalette />
      <DocsPanel />
      <GlobalShortcuts />
    </QueryClientProvider>
  </StrictMode>,
);
