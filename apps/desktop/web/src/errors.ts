/**
 * Copyright 2026 Retail Cortex
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Reading the service's errors: its message, and the ErrorInfo reason that
// says which one it is.
import { Code, ConnectError } from "@connectrpc/connect";
import { ErrorInfoSchema } from "./gen/blitz/v1/turn_pb";

/** The text of an error: the service's message for its errors, else the error's own. */
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
