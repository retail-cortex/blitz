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

// Studio's skill form (spec_skills_013 SK-100–SK-103): the fields as the
// form holds them, from and to the API, and the rules for names and files.

import type { SkillFile, SkillInfo } from "../gen/blitz/v1/workspace_pb";

/** What the form holds. Tools are a list here, allowed-tools (space-separated) in SKILL.md. */
export interface SkillForm {
  name: string;
  description: string;
  tools: string[];
  agents: string[];
  members: string[];
  instructions: string;
  /** Trigger phrases: requests it's for, which the model matches by meaning (SK-110). */
  triggers: string[];
}

/** A new skill's form. */
export const emptySkillForm: SkillForm = { name: "", description: "", tools: [], agents: [], members: [], instructions: "", triggers: [] };

/** The form for a skill as read. */
export function formFromSkill(f: SkillFile): SkillForm {
  const s = f.spec;
  return { name: s?.name ?? "", description: s?.description ?? "", tools: (s?.tools ?? "").split(/\s+/).filter(Boolean), agents: [...(s?.agents ?? [])], members: [...(s?.members ?? [])], instructions: s?.instructions ?? "", triggers: [...(s?.triggers ?? [])] };
}

/** The form as SaveSkill takes it. */
export function specFromForm(f: SkillForm) {
  return { name: f.name.trim(), description: f.description.trim(), tools: f.tools.join(" "), agents: f.agents, members: f.members, instructions: f.instructions, triggers: f.triggers };
}

/** Whether two forms hold the same skill. */
export function sameSkillForm(a: SkillForm, b: SkillForm): boolean {
  return JSON.stringify(specFromForm(a)) === JSON.stringify(specFromForm(b));
}

/** A trigger phrase as typed, tidied ("" when blank); the list it joins, without it twice. */
export function addTrigger(list: readonly string[], typed: string): string[] {
  const p = typed.trim().replace(/\s+/g, " ");
  return p && !list.includes(p) ? [...list, p] : [...list];
}

/** A scenario's name made from its prompt ("Write the release notes" → write-the-release-notes), at most five words. */
export function scenarioName(prompt: string, taken: readonly string[]): string {
  const base = skillName(prompt.split(/\s+/).slice(0, 5).join(" ")) || "scenario";
  let name = base;
  for (let n = 2; taken.includes(name); n++) name = `${base}-${n}`;
  return name;
}

/** Whether name may name a skill: lowercase letters, digits and single hyphens, up to 64. */
export function validSkillName(name: string): boolean {
  return name.length <= 64 && /^[a-z0-9]+(-[a-z0-9]+)*$/.test(name);
}

/** A skill's name made from what's typed ("Release notes!" → release-notes). */
export function skillName(text: string): string {
  return text
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 64)
    .replace(/-+$/, "");
}

/** The kinds of skill (SK-97), in the order the form describes them. */
export const skillKinds = ["simple", "references", "scripts", "meta"] as const;
/** A kind of skill. */
export type SkillKind = (typeof skillKinds)[number];

/** A skill's kind, as the API says it (simple when it doesn't). */
export function kindOf(s: Pick<SkillInfo, "kind">): SkillKind {
  return (skillKinds as readonly string[]).includes(s.kind) ? (s.kind as SkillKind) : "simple";
}

/** The languages a script can be written in, with their extensions (SK-105, SK-106: shell, cmd and PowerShell run with the system's shells). */
export const scriptLanguages = { python: ".py", typescript: ".ts", javascript: ".js", shell: ".sh", cmd: ".cmd", powershell: ".ps1" } as const;
/** A script's language. */
export type ScriptLanguage = keyof typeof scriptLanguages;

/** A script file's language, from its extension ("" for neither). */
export function languageOfScript(path: string): ScriptLanguage | "" {
  const ext = path.slice(path.lastIndexOf(".")).toLowerCase();
  const by: Record<string, ScriptLanguage> = { ".py": "python", ".ts": "typescript", ".js": "javascript", ".mjs": "javascript", ".sh": "shell", ".cmd": "cmd", ".bat": "cmd", ".ps1": "powershell" };
  return by[ext] ?? "";
}

/** A new file's path in a skill's folder, from the name typed: under folder, with ext when it has none; "" when it won't do. */
export function filePath(folder: "references" | "scripts", name: string, ext: string): string {
  const n = name.trim().replace(/^\/+/, "");
  if (!n || n.includes("..") || n.includes("\\") || n.startsWith(".")) return "";
  return `${folder}/${/\.[A-Za-z0-9]+$/.test(n) ? n : n + ext}`;
}

/** A new script's first lines: what it is, and where its arguments come in. */
export function scriptTemplate(language: ScriptLanguage, name: string): string {
  switch (language) {
    case "python":
      return `"""${name}: what it does."""\n\nimport sys\n\n\ndef main(args: list[str]) -> int:\n    print("args:", args)\n    return 0\n\n\nif __name__ == "__main__":\n    sys.exit(main(sys.argv[1:]))\n`;
    case "typescript":
      return `// ${name}: what it does.\n\nconst args = process.argv.slice(2);\nconsole.log("args:", args);\n`;
    case "shell":
      return `#!/bin/sh\n# ${name}: what it does.\nset -eu\n\necho "args: $*"\n`;
    case "cmd":
      return `@echo off\nrem ${name}: what it does.\n\necho args: %*\n`;
    case "javascript":
      return `// ${name}: what it does.\n\nconst args = process.argv.slice(2);\nconsole.log("args:", args);\n`;
    case "powershell":
      return `# ${name}: what it does.\n$ErrorActionPreference = "Stop"\n\nWrite-Output "args: $($args -join ' ')"\n`;
  }
}

/** A package file's kind: python (requirements.txt, pyproject.toml) or node (package.json). */
export const packageKind = (path: string): "python" | "node" => (path.endsWith("package.json") ? "node" : "python");

/** The package files that give scripts their packages: the first of each kind (SK-107). */
export function activePackageFiles(files: readonly string[]): Set<string> {
  const out = new Set<string>();
  for (const kind of ["python", "node"] as const) {
    const first = files.find((f) => packageKind(f) === kind);
    if (first) out.add(first);
  }
  return out;
}

/** A new package file's first lines. */
export function packageTemplate(path: string): string {
  if (path.endsWith("package.json")) return `{\n  "dependencies": {\n  }\n}\n`;
  if (path.endsWith("pyproject.toml")) return `[project]\nname = "skill-scripts"\nversion = "0"\ndependencies = [\n]\n`;
  return "# One package a line, e.g.\n# requests>=2.31\n";
}

/** A test run's arguments, split as a shell would split simple words and "quoted text". */
export function splitArgs(line: string): string[] {
  const out: string[] = [];
  for (const m of line.matchAll(/"([^"]*)"|'([^']*)'|(\S+)/g)) out.push(m[1] ?? m[2] ?? m[3]);
  return out;
}
