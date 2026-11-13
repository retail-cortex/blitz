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

import { t } from "../i18n";

// Personalities in the page (spec_064_personality): how the root agent
// sounds. The service checks what's saved (engine.CheckPersonality); the
// page checks the same as the person types, so Save says why before it's
// pressed.

/** A personality as the page has it (the proto's fields it uses). */
export type PersonalityLike = { name: string; description: string; voice: string; source: string; text?: string };

/** The built-ins, in the order the service lists them (default first). */
export const builtinPersonalities = ["default", "calm", "cheerful", "direct", "friendly", "mentor", "reviewer"] as const;

/** How long a personality's text may be (engine.MaxPersonalityText). */
export const maxPersonalityText = 2000;

/** Whether name can name a personality (config.ValidPersonalityName). */
export const validPersonalityName = (name: string) => name.length <= 40 && /^[a-z0-9][a-z0-9-]*$/.test(name);

const isBuiltIn = (p: PersonalityLike) => p.source === "built-in" && (builtinPersonalities as readonly string[]).includes(p.name);

/** A personality's name to show: a built-in's in the page's language, else its own. */
export const personalityLabel = (p: PersonalityLike) => (isBuiltIn(p) ? t(`desktop.personality.name.${p.name}`) : p.name);

/** Its description to show: a built-in's in the page's language, else its own. */
export const personalityDescription = (p: PersonalityLike) => (isBuiltIn(p) ? t(`desktop.personality.desc.${p.name}`) : p.description);

/** Where it's from, to show beside it. */
export const personalitySource = (p: PersonalityLike) => t(`desktop.personality.source.${p.source === "own" ? "own" : p.source === "organization" ? "organization" : "built_in"}`);

/**
 * Why the editor's personality can't be saved, or "": the service's
 * checks (CheckPersonality), a name another of the person's has (unless
 * it's the one being edited), and secrets left to the service.
 */
export function personalityProblem(p: { name: string; description: string; text: string }, own: readonly string[], editing: string): string {
  const name = p.name.trim();
  if (!name) return t("desktop.personality.problem.no_name");
  if (!validPersonalityName(name)) return t("desktop.personality.problem.bad_name");
  if (name === "default") return t("desktop.personality.problem.default");
  if (name !== editing && own.includes(name)) return t("desktop.personality.problem.taken", { name });
  if (/[\n\r]/.test(p.description)) return t("desktop.personality.problem.description");
  if (!p.text.trim()) return t("desktop.personality.problem.no_text");
  if ([...p.text].length > maxPersonalityText) return t("desktop.personality.problem.long", { max: maxPersonalityText });
  return "";
}

/** A name for a copy of name, not one of taken: name-2, name-3… */
export function copyName(name: string, taken: readonly string[]): string {
  const base = name.replace(/-\d+$/, "").slice(0, 36) || "personality";
  for (let n = 2; ; n++) {
    const next = `${base}-${n}`;
    if (!taken.includes(next)) return next;
  }
}

/** The status bar's label for the agent with personality p: "Blitz · Mentor", or the agent alone for default. */
export const agentWithPersonality = (agent: string, p: PersonalityLike | undefined) => (p && p.name !== "default" ? `${agent} · ${personalityLabel(p)}` : agent);
