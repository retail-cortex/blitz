// Colours a workspace can be given, shown as its dot in the drawer and its
// accent in the top bar. Each works on light and dark surfaces.

export const palette: Record<string, { light: string; dark: string; label: string }> = {
  blue: { light: "#0b57d0", dark: "#a8c7fa", label: "Blue" },
  teal: { light: "#00696f", dark: "#6fd7df", label: "Teal" },
  green: { light: "#146c2e", dark: "#6dd58c", label: "Green" },
  amber: { light: "#8b5000", dark: "#ffb870", label: "Amber" },
  red: { light: "#b3261e", dark: "#f2b8b5", label: "Red" },
  pink: { light: "#a4176f", dark: "#ffafd9", label: "Pink" },
  purple: { light: "#6750a4", dark: "#d0bcff", label: "Purple" },
  grey: { light: "#5f6368", dark: "#bdc1c6", label: "Grey" },
};

export const colorNames = Object.keys(palette);

/** The colour for a workspace's colour name (blue when unset or unknown). */
export function workspaceColor(name: string | undefined, theme: "light" | "dark"): string {
  const c = palette[name ?? ""] ?? palette.blue;
  return c[theme];
}
