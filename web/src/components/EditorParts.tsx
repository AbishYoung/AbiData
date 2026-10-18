// Shared building blocks for the object editors that open as workspace tabs
// (see DESIGN.md: "Document toolbar", "Every empty state is designed"). The components now live in
// components/ui (ADR 0015); these names are kept for existing imports.
export { docFooter, docToolbar } from "./styles";
export { EmptyState, Skeleton as SkeletonLines } from "./ui/EmptyState";
export { ToolbarSeparator as ToolbarDivider } from "./ui/Toolbar";
