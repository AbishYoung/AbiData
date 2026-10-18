import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

// Design-system guards (ADR 0015, DESIGN.md). Colours come from tokens.css; the documented
// exceptions (white on a solid brand, kind or accent fill; the photo viewer's overlay; the Monaco
// fallbacks) carry an eslint-disable comment with the reason.
const PALETTE =
  "\\b(bg|text|border|ring|from|to|via|fill|stroke|outline|divide|shadow|caret|decoration|placeholder)-(slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|black|white)\\b";
const HEX = "#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?([0-9a-fA-F]{2})?\\b";
const designSystem = [
  {
    selector: "JSXOpeningElement[name.type='JSXIdentifier'][name.name=/^[a-z]/] > JSXAttribute[name.name='title']",
    message: "Use <Tip> (components/Tip.tsx) instead of the native title attribute (DESIGN.md, Tooltips).",
  },
  { selector: `Literal[value=/${PALETTE}/]`, message: "Use the design tokens (bg-surface, text-fg-muted …), not Tailwind palette colours." },
  { selector: `TemplateElement[value.raw=/${PALETTE}/]`, message: "Use the design tokens (bg-surface, text-fg-muted …), not Tailwind palette colours." },
  { selector: `Literal[value=/${HEX}/]`, message: "Define colours in styles/tokens.css and use the token." },
  { selector: `TemplateElement[value.raw=/${HEX}/]`, message: "Define colours in styles/tokens.css and use the token." },
];
const nativeDialogs = ["confirm", "alert", "prompt"].map((name) => ({
  name,
  message: "Use askToConfirm or ConfirmDialog (components/ui) instead of the browser's dialogs.",
}));

export default tseslint.config(
  { ignores: ["dist", "node_modules", "src/generated"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.strict],
    files: ["**/*.{ts,tsx}"],
    languageOptions: { ecmaVersion: 2023, globals: globals.browser },
    plugins: { "react-hooks": reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/no-non-null-assertion": "error",
      // Only relevant with the React Compiler (not used): TanStack Virtual's hooks
      // return non-memoizable functions by design.
      "react-hooks/incompatible-library": "off",
      "no-restricted-syntax": ["error", ...designSystem],
      "no-restricted-globals": ["error", ...nativeDialogs],
      "no-restricted-properties": ["error", ...nativeDialogs.map((d) => ({ object: "window", property: d.name, message: d.message }))],
    },
  },
  {
    // Tests build fixtures with literal colours and stub the browser's dialogs.
    files: ["**/*.test.{ts,tsx}"],
    rules: { "no-restricted-syntax": "off", "no-restricted-properties": "off" },
  },
);
