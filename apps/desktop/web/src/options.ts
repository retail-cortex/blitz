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

// The choices the run settings offer, with what each means (translated
// when asked for, so a language change shows at once).
import { mdiBrain, mdiFileEditOutline, mdiHandBackRight, mdiLockOutline, mdiRobotOutline, mdiShieldCheckOutline, mdiShieldOffOutline } from "@mdi/js";
import { t } from "./i18n";

/** A choice in a settings menu: its value, name, explanation and icon. */
export interface Option {
  value: string;
  label: string;
  detail: string;
  icon?: string;
}

const modeIcons: Record<string, string> = {
  default: mdiShieldCheckOutline,
  "accept-edits": mdiFileEditOutline,
  auto: mdiRobotOutline,
  plan: mdiLockOutline,
  "dont-ask": mdiHandBackRight,
  bypass: mdiShieldOffOutline,
};

/** The permission modes. */
export const modes = (): Option[] =>
  Object.keys(modeIcons).map((value) => ({ value, icon: modeIcons[value], label: t(`desktop.mode.${value}`), detail: t(`desktop.mode.${value}.detail`) }));

/** The permission mode option for a value (the default mode if unknown). */
export const modeOf = (v: string) => modes().find((m) => m.value === v) ?? modes()[0];

/** The permission modes a workflow can run in, auto (its default) first: plan isn't one (WF-21). */
export const flowModes = (): Option[] => ["auto", "accept-edits", "default", "dont-ask", "bypass"].map((v) => modeOf(v));

const modeRank: Record<string, number> = { plan: 0, "dont-ask": 1, default: 2, "accept-edits": 3, auto: 3, bypass: 4 };

/** Whether mode a lets more through than b (accept-edits and auto are level), as the service holds a workflow's mode back. */
export const looser = (a: string, b: string) => (modeRank[a] ?? 2) > (modeRank[b] ?? 2);

/** The reasoning efforts ("" is Auto). */
export const efforts = (): Option[] =>
  ["", "minimal", "low", "medium", "high", "max"].map((value) => ({
    value,
    label: t(`desktop.effort.${value || "auto"}`),
    detail: t(`desktop.effort.${value || "auto"}.detail`),
  }));

/** The icon for reasoning effort. */
export const effortIcon = mdiBrain;

/** The agency levels. */
export const agencies = (): Option[] =>
  ["low", "medium", "high", "extreme"].map((value) => ({ value, label: t(`desktop.agency.${value}`), detail: t(`desktop.agency.${value}.detail`) }));
