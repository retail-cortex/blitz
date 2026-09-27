// Reading the service's errors: its message, and the ErrorInfo reason that
// says which one it is.
import { Code, ConnectError } from "@connectrpc/connect";
import { ErrorInfoSchema } from "./gen/blitz/v1/turn_pb";

export function message(e: unknown): string {
  if (e instanceof ConnectError) return e.rawMessage;
  return e instanceof Error ? e.message : String(e);
}

/** The service's reason for an error ("" when it has none). */
export function reason(e: unknown): string {
  if (!(e instanceof ConnectError)) return "";
  return e.findDetails(ErrorInfoSchema)[0]?.reason ?? "";
}

/** A value of the error's ErrorInfo metadata ("" when it has none). */
export function errorMeta(e: unknown, key: string): string {
  if (!(e instanceof ConnectError)) return "";
  return e.findDetails(ErrorInfoSchema)[0]?.metadata[key] ?? "";
}

/** Whether the service couldn't be reached (it stopped, is stopping, or isn't running). */
export function isUnavailable(e: unknown): boolean {
  return e instanceof ConnectError && e.code === Code.Unavailable;
}
