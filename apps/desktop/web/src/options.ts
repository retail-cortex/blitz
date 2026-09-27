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
import { mdiBrain, mdiFileEditOutline, mdiHandBackRight, mdiLockOutline, mdiShieldCheckOutline, mdiShieldOffOutline } from "@mdi/js";
import { t } from "./i18n";

export interface Option {
  value: string;
  label: string;
  detail: string;
  icon?: string;
}

const modeIcons: Record<string, string> = {
  default: mdiShieldCheckOutline,
  "accept-edits": mdiFileEditOutline,
  plan: mdiLockOutline,
  "dont-ask": mdiHandBackRight,
  bypass: mdiShieldOffOutline,
};

/** The permission modes. */
export const modes = (): Option[] =>
  Object.keys(modeIcons).map((value) => ({ value, icon: modeIcons[value], label: t(`desktop.mode.${value}`), detail: t(`desktop.mode.${value}.detail`) }));

export const modeOf = (v: string) => modes().find((m) => m.value === v) ?? modes()[0];

/** The reasoning efforts ("" is Auto). */
export const efforts = (): Option[] =>
  ["", "minimal", "low", "medium", "high", "max"].map((value) => ({
    value,
    label: t(`desktop.effort.${value || "auto"}`),
    detail: t(`desktop.effort.${value || "auto"}.detail`),
  }));

export const effortIcon = mdiBrain;

/** The agency levels. */
export const agencies = (): Option[] =>
  ["low", "medium", "high", "extreme"].map((value) => ({ value, label: t(`desktop.agency.${value}`), detail: t(`desktop.agency.${value}.detail`) }));
