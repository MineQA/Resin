import type { CSSProperties } from "react";

// Shared protocol pill styles used by the node pool list/export filters and
// the platform protocol filters. Selected pills use the primary brand color so
// the active state is unmistakable against the unselected outline style.
// Theme variables referenced here are defined in src/styles/theme.css:
//   --primary, --primary-strong, --primary-soft, --border, --text, --text-muted

// Unselected pill: subtle outlined chip, legible muted text.
export const PROTOCOL_PILL_STYLE: CSSProperties = {
  display: "inline-flex",
  alignItems: "center",
  padding: "2px 8px",
  fontSize: "0.72rem",
  fontWeight: 500,
  borderRadius: "4px",
  cursor: "pointer",
  border: "1px solid var(--border)",
  background: "transparent",
  color: "var(--text-muted)",
  lineHeight: 1.4,
  whiteSpace: "nowrap",
  userSelect: "none",
  transition: "background 0.15s ease, color 0.15s ease, border-color 0.15s ease, box-shadow 0.15s ease",
};

// Selected pill: solid primary fill, white text, stronger border, bold, with a
// subtle shadow so it lifts off the row and reads as clearly active.
export const PROTOCOL_PILL_SELECTED_STYLE: CSSProperties = {
  ...PROTOCOL_PILL_STYLE,
  background: "var(--primary)",
  color: "#ffffff",
  borderColor: "var(--primary-strong)",
  fontWeight: 600,
  boxShadow: "0 1px 3px rgba(20, 112, 255, 0.28)",
};

// Row container that wraps the pill group.
export const PROTOCOL_PILL_ROW_STYLE: CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  gap: "4px",
  padding: "2px 0",
};
