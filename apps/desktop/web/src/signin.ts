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

// Which provider sign-in a setup uses (SignInPanel.tsx): the auth
// method's (adc, and Gemini's oauth: Google Cloud; Claude's oauth: an
// Anthropic account), or a cloud
// provider's (Bedrock: AWS; Azure: Entra ID; Claude on Vertex: Google).

/** The sign-in for an auth method and provider ("" for none: an API key). */
export function signInProvider(method: string, provider: string): string {
  if (method === "adc" || provider === "vertex-anthropic" || (method === "oauth" && provider === "gemini")) return "google";
  if (method === "oauth" && provider === "anthropic") return "anthropic";
  if (provider === "bedrock") return "aws";
  if (provider === "azure") return "azure";
  return "";
}
