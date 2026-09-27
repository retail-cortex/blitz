// The choices the run settings offer, with what each means.
import { mdiBrain, mdiFileEditOutline, mdiHandBackRight, mdiLockOutline, mdiShieldCheckOutline, mdiShieldOffOutline } from "@mdi/js";

export const modes = [
  { value: "default", label: "Ask", detail: "Ask before edits, commands and other sensitive actions", icon: mdiShieldCheckOutline },
  { value: "accept-edits", label: "Accept edits", detail: "Change files in the workspace without asking; commands still ask", icon: mdiFileEditOutline },
  { value: "plan", label: "Plan", detail: "Plan only: tools that change anything are refused until you approve a plan", icon: mdiLockOutline },
  { value: "dont-ask", label: "Don't ask", detail: "Never ask: anything not already allowed is refused", icon: mdiHandBackRight },
  { value: "bypass", label: "Bypass", detail: "Ask for nothing (only inside the OS sandbox; deny rules still apply)", icon: mdiShieldOffOutline },
];

export const modeOf = (v: string) => modes.find((m) => m.value === v) ?? modes[0];

export const efforts = [
  { value: "", label: "Auto", detail: "Each model's own setting" },
  { value: "minimal", label: "Minimal", detail: "Answer with as little thinking as possible" },
  { value: "low", label: "Low", detail: "Think briefly" },
  { value: "medium", label: "Medium", detail: "Think moderately" },
  { value: "high", label: "High", detail: "Think carefully" },
  { value: "max", label: "Max", detail: "Think as long as it helps (slower, costlier)" },
];

export const effortIcon = mdiBrain;

export const agencies = [
  { value: "low", label: "Low", detail: "Stop after each meaningful unit of work and ask" },
  { value: "medium", label: "Medium", detail: "Do routine tasks; pause at major milestones" },
  { value: "high", label: "High", detail: "Work autonomously; ask only when blocked" },
  { value: "extreme", label: "Extreme", detail: "Maximally autonomous, polling background tasks" },
];
