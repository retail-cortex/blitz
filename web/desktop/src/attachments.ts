// Images attached to the next prompt: pasted, dropped or chosen, uploaded
// to the service at once (AddImage), sent with the turn by ID.

/** The largest image the window uploads (the service may scale it down). */
export const maxImageBytes = 20 << 20;

export interface Attachment {
  key: string; // local, for the list
  name: string;
  url: string; // a local object URL, for the thumbnail
  id?: string; // the service's ID once uploaded
  detail?: string; // size and dimensions, once uploaded
  error?: string;
}

/** The image files among files (paste and drop carry other kinds too). */
export function imageFiles(files: Iterable<File> | ArrayLike<File> | null | undefined): File[] {
  return Array.from(files ?? []).filter((f) => f.type.startsWith("image/"));
}

/** Why a file can't be attached ("" if it can). */
export function rejectReason(f: Pick<File, "type" | "size" | "name">): string {
  if (!f.type.startsWith("image/")) return `${f.name} isn't an image`;
  if (f.size > maxImageBytes) return `${f.name} is larger than ${maxImageBytes >> 20} MB`;
  return "";
}

/** A short description of an uploaded image. */
export function describeImage(i: { width: number; height: number; size: bigint | number; resized: boolean }): string {
  const bytes = Number(i.size);
  const size = bytes >= 1 << 20 ? `${(bytes / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB`;
  return `${i.width}×${i.height} · ${size}${i.resized ? " · scaled down" : ""}`;
}

/** The IDs to send: uploaded images only. */
export const readyIds = (list: Attachment[]) => list.filter((a) => a.id && !a.error).map((a) => a.id!);

/** Whether any image is still uploading. */
export const uploading = (list: Attachment[]) => list.some((a) => !a.id && !a.error);
