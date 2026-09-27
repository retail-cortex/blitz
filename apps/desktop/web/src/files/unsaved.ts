// Unsaved changes across the window's workspaces, for closing the window
// (the app asks first, spec_files_029 FIL-45).
import { setUnsaved } from "../desktop";
import { t, tn } from "../i18n";

const counts = new Map<string, number>();

/** Records a workspace's number of files with unsaved changes. */
export function reportUnsaved(dir: string, count: number) {
  if ((counts.get(dir) ?? 0) === count) return;
  if (count) counts.set(dir, count);
  else counts.delete(dir);
  const total = [...counts.values()].reduce((a, b) => a + b, 0);
  setUnsaved(total ? tn("desktop.files.quit_unsaved", total) : "", t("desktop.files.quit"), t("desktop.cancel"));
}

/** The number of files with unsaved changes in a workspace. */
export function unsavedIn(dir: string): number {
  return counts.get(dir) ?? 0;
}
