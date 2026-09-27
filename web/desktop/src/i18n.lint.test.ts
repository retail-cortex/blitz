// No text the user reads may be written into the page's code: it comes
// from the catalogs through t() and tn(). The page's counterpart of the
// terminal's TestNoUntranslatedOutput (I18N-06). It reads the sources
// with the TypeScript compiler and reports, with file and line:
//   - JSX text with letters in it;
//   - string or template literals with letters in title, placeholder,
//     aria-label, label and alt attributes, and as JSX children;
//   - literals passed to snack(), say(), setError(), tell() and notify();
//   - literals as label, detail, description, heading or title fields of
//     object literals (menus, options, palette items).
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import ts from "typescript";
import { describe, expect, it } from "vitest";
import enUS from "../../../internal/i18n/locales/en-US.json";

const dir = import.meta.dirname;
const skip = (f: string) => f.endsWith(".test.ts") || f.endsWith(".d.ts") || f === "i18n.ts" || f.startsWith("gen") || f.startsWith("dev");

function sources(d: string, rel = ""): string[] {
  return readdirSync(d, { withFileTypes: true }).flatMap((e) => {
    const r = rel ? `${rel}/${e.name}` : e.name;
    if (skip(r)) return [];
    if (e.isDirectory()) return sources(join(d, e.name), r);
    return /\.(ts|tsx)$/.test(e.name) ? [r] : [];
  });
}

const letters = /[A-Za-z]{2,}/;
const attrs = new Set(["title", "placeholder", "aria-label", "label", "alt", "supporting", "help", "detail"]);
const calls = new Set(["snack", "say", "setError", "tell", "notify"]);
const fields = new Set(["label", "detail", "description", "heading", "title"]);

/** The text of a string or template literal (its fixed parts), else undefined. */
function literal(n: ts.Node): string | undefined {
  if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
  if (ts.isTemplateExpression(n)) return n.head.text + n.templateSpans.map((s) => s.literal.text).join(" ");
  if (ts.isConditionalExpression(n)) return [literal(n.whenTrue), literal(n.whenFalse)].filter(Boolean).join(" ") || undefined;
  if (ts.isBinaryExpression(n) && n.operatorToken.kind === ts.SyntaxKind.PlusToken) return [literal(n.left), literal(n.right)].filter(Boolean).join(" ") || undefined;
  if (ts.isParenthesizedExpression(n)) return literal(n.expression);
  return undefined;
}

function problems(file: string): string[] {
  const text = readFileSync(join(dir, file), "utf8");
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, file.endsWith("x") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const out: string[] = [];
  const report = (n: ts.Node, what: string) => out.push(`${file}:${sf.getLineAndCharacterOfPosition(n.getStart()).line + 1}: ${what}`);
  const check = (n: ts.Node | undefined, where: string) => {
    if (!n) return;
    const s = literal(n);
    // Permission-rule syntax (shell(npm test)) is code, not text.
    if (s && letters.test(s) && !/^[a-z_]+\(.*\)$/.test(s)) report(n, `${where}: ${JSON.stringify(s.slice(0, 60))}`);
  };
  const visit = (n: ts.Node) => {
    if (ts.isJsxText(n) && letters.test(n.text)) report(n, `JSX text ${JSON.stringify(n.text.trim().slice(0, 60))}`);
    if (ts.isJsxAttribute(n) && attrs.has(n.name.getText(sf)) && n.initializer) {
      const init = n.initializer;
      check(ts.isJsxExpression(init) ? init.expression : init, `${n.name.getText(sf)}=`);
    }
    if (ts.isJsxExpression(n) && n.expression && (ts.isJsxElement(n.parent) || ts.isJsxFragment(n.parent))) check(n.expression, "JSX child");
    if (ts.isCallExpression(n)) {
      const name = ts.isIdentifier(n.expression) ? n.expression.text : ts.isPropertyAccessExpression(n.expression) ? n.expression.name.text : "";
      // tell(kind, title, body) and notify(title, body, dir): the texts only.
      const texts = name === "tell" ? n.arguments.slice(1, 3) : name === "notify" ? n.arguments.slice(0, 2) : n.arguments.slice(0, 1);
      if (calls.has(name)) texts.forEach((a) => check(a, `${name}()`));
    }
    if (ts.isPropertyAssignment(n) && fields.has(n.name.getText(sf)) && ts.isObjectLiteralExpression(n.parent)) check(n.initializer, `${n.name.getText(sf)}:`);
    ts.forEachChild(n, visit);
  };
  visit(sf);
  return out;
}

/** The desktop keys the sources use in t("…") and tn("…"). */
function usedKeys(): string[] {
  const keys = new Set<string>();
  for (const f of sources(dir)) {
    for (const m of readFileSync(join(dir, f), "utf8").matchAll(/\bt\(\s*"(desktop\.[\w.-]+)"/g)) keys.add(m[1]);
    for (const m of readFileSync(join(dir, f), "utf8").matchAll(/\btn\(\s*"(desktop\.[\w.-]+)"/g)) keys.add(m[1] + ".other");
  }
  return [...keys];
}

describe("the page's text", () => {
  it("comes from the catalogs", () => {
    expect(sources(dir).flatMap(problems)).toEqual([]);
  });
  it("uses keys that exist in the English catalog", () => {
    const messages = (enUS as { messages: Record<string, string> }).messages;
    expect(usedKeys().filter((k) => !(k in messages))).toEqual([]);
  });
});
