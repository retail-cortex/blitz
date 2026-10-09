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


// The visual editor (spec_visual_editor_037 §3): a Markdown file edited in
// its rendered form, in the document's own style (FIL-57), its code blocks
// CodeMirror editors. It reads the tab's text and gives back Markdown
// faithful to the file (VE-04): the tab keeps the text, so saving, unsaved
// changes and conflicts work as in Source.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { getSchema, type Editor, type JSONContent, type NodeViewRendererProps } from "@tiptap/core";
import { EditorContent, useEditor } from "@tiptap/react";
import type { Node as PMNode, Slice } from "@tiptap/pm/model";
import { NodeSelection } from "@tiptap/pm/state";
import {
  mdiCheckboxMarkedOutline,
  mdiCodeBraces,
  mdiCodeTags,
  mdiFormatBold,
  mdiFormatHeader1,
  mdiFormatHeader2,
  mdiFormatHeader3,
  mdiFormatItalic,
  mdiFormatListBulleted,
  mdiFormatListNumbered,
  mdiFormatQuoteClose,
  mdiFormatStrikethroughVariant,
  mdiLinkVariant,
  mdiLinkVariantOff,
  mdiMinus,
  mdiSitemapOutline,
  mdiTable,
  mdiTableColumnPlusAfter,
  mdiTableColumnRemove,
  mdiTableRemove,
  mdiTableRowPlusAfter,
  mdiTableRowRemove,
} from "@mdi/js";
import { t } from "../../i18n";
import { followLink } from "../../Markdown";
import { Icon, IconButton, useSnackbar } from "../../ui/controls";
import { CodeBlockView } from "./codeBlock";
import { fingerprints, parseMarkdown, serializeMarkdown, type Parsed } from "./markdown";
import { sourceAttr, visualExtensions } from "./schema";
import "./visual.css";

/** How long typing waits before the Markdown is written (VE-06). */
const writeDelay = 150;

// The extensions with their views: code and raw blocks as CodeMirror,
// images as Preview's boxes (never loaded, DSK-24).
function editorExtensions() {
  return visualExtensions().map((ext) => {
    switch (ext.name) {
      case "codeBlock":
        return ext.extend({ addNodeView: () => ({ node, view, getPos }: NodeViewRendererProps) => new CodeBlockView(node, view, getPos, "code") });
      case "rawBlock":
        return ext.extend({ addNodeView: () => ({ node, view, getPos }: NodeViewRendererProps) => new CodeBlockView(node, view, getPos, "raw") });
      case "image":
        return ext.extend({
          renderHTML: ({ node }: { node: PMNode }) => ["span", { class: "md-image", "data-src": node.attrs.src as string, title: node.attrs.src as string }, (node.attrs.alt as string) || (node.attrs.src as string)],
        });
    }
    return ext;
  });
}

const schema = getSchema(visualExtensions());

// Blocks pasted or written from a slice have no source to keep.
function withoutSources(nodes: JSONContent[]): JSONContent[] {
  return nodes.map((n) => (n.attrs && sourceAttr in n.attrs ? { ...n, attrs: { ...n.attrs, [sourceAttr]: null } } : n));
}

/** A slice as Markdown, for the clipboard (DSK-72a's Markdown form). */
export function sliceMarkdown(slice: Slice): string {
  try {
    const doc = schema.topNodeType.create(null, slice.content);
    return serializeMarkdown(doc, { sources: new Map(), trailing: "" }, new Map()).replace(/\n+$/, "");
  } catch {
    return slice.content.textBetween(0, slice.content.size, "\n\n");
  }
}

// What Markdown text looks like, pasted: block syntax at a line's start,
// or inline syntax around words.
const looksLikeMarkdown = /(^|\n)\s{0,3}(#{1,6}\s|[-*+]\s|\d+[.)]\s|>|```|~~~|\|)|\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\([^)]+\)/;

interface Props {
  dir: string;
  path: string;
  /** The tab's text. */
  text: string;
  /** Markdown written back, as the document changes. */
  onChange: (text: string) => void;
  onSave: () => void;
}

/** A Markdown file, edited in its rendered form. */
export function VisualEditor({ dir, path, text, onChange, onSave }: Props) {
  const snack = useSnackbar();
  const parsed = useRef<Parsed | null>(null);
  const prints = useRef(new Map<string, string>());
  const written = useRef<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const change = useRef(onChange);
  change.current = onChange;
  const save = useRef(onSave);
  save.current = onSave;
  const extensions = useMemo(editorExtensions, []);
  const [slash, setSlash] = useState<SlashState | null>(null);
  const slashRef = useRef<SlashState | null>(null);
  slashRef.current = slash;
  const [linking, setLinking] = useState<LinkState | null>(null);

  // Markdown out: only when it says something new.
  const write = useCallback((editor: Editor) => {
    clearTimeout(timer.current);
    const p = parsed.current;
    if (!p) return;
    const md = serializeMarkdown(editor.state.doc, p, prints.current);
    if (md === written.current) return;
    written.current = md;
    change.current(md);
  }, []);

  const editor = useEditor({
    extensions,
    injectCSS: false, // the app's styles only (VE-15)
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    editorProps: {
      attributes: { class: "markdown visual-doc", spellcheck: "true", "aria-label": t("desktop.visual.label") },
      clipboardTextSerializer: sliceMarkdown,
      handlePaste: (view, event) => {
        const data = event.clipboardData;
        const plain = data?.getData("text/plain") ?? "";
        if (!data || data.getData("text/html") || !looksLikeMarkdown.test(plain)) return false; // HTML: cleaned to the schema by ProseMirror
        const content = withoutSources(parseMarkdown(plain).doc.content ?? []);
        const node = schema.nodeFromJSON({ type: "doc", content });
        view.dispatch(view.state.tr.replaceSelection(node.slice(0, node.content.size)).scrollIntoView());
        return true;
      },
      handleClick: (view, pos, event) => {
        if (!(event.metaKey || event.ctrlKey)) return false;
        const link = view.state.doc.resolve(pos).marks().find((m) => m.type.name === "link");
        if (!link) return false;
        followLink(link.attrs.href as string, { dir, path }, view.dom, (m) => snack(m, { error: true }));
        return true;
      },
      handleKeyDown: (_view, event) => slashKeys(event),
    },
    onUpdate: ({ editor }) => {
      clearTimeout(timer.current);
      timer.current = setTimeout(() => write(editor), writeDelay);
    },
    onBlur: ({ editor }) => write(editor),
    onTransaction: ({ editor }) => setSlash(slashAt(editor)),
  });

  // The tab's text in: the first time, and when it changes other than by
  // this editor (a reload from disk, Source's edits).
  useLayoutEffect(() => {
    if (!editor || text === written.current) return;
    const p = parseMarkdown(text);
    editor.commands.setContent(p.doc, { emitUpdate: false });
    parsed.current = p;
    prints.current = fingerprints(editor.state.doc);
    written.current = text;
  }, [editor, text]);

  // Written before it goes (a switch to Source, another tab, the window).
  useEffect(() => () => {
    if (editor && !editor.isDestroyed) write(editor);
  }, [editor, write]);

  // ⌘S: written, then saved; the code blocks ask for it the same way.
  useEffect(() => {
    const dom = editor?.view.dom;
    if (!dom) return;
    const onSave = () => {
      write(editor);
      save.current();
    };
    dom.addEventListener("visual-save", onSave);
    return () => dom.removeEventListener("visual-save", onSave);
  }, [editor, write]);

  // The / menu's keys, while it's open.
  const slashKeys = (event: KeyboardEvent): boolean => {
    if ((event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey) {
      if (event.key === "s") {
        event.preventDefault();
        if (editor) write(editor);
        save.current();
        return true;
      }
      if (event.key === "k" && editor) {
        event.preventDefault();
        setLinking(linkAt(editor));
        return true;
      }
    }
    const s = slashRef.current;
    if (!s || !editor) return false;
    const items = slashItems(s.query);
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      const n = items.length || 1;
      setSlash({ ...s, index: (s.index + (event.key === "ArrowDown" ? 1 : n - 1)) % n });
      return true;
    }
    if ((event.key === "Enter" || event.key === "Tab") && items[s.index]) {
      runSlash(editor, s, items[s.index]);
      return true;
    }
    if (event.key === "Escape") {
      setSlash(null);
      return true;
    }
    return false;
  };

  if (!editor) return null;
  return (
    <div className="visual-editor">
      <EditorContent editor={editor} />
      {slash && <SlashMenu editor={editor} state={slash} onPick={(item) => runSlash(editor, slash, item)} />}
      <SelectionBar editor={editor} onLink={() => setLinking(linkAt(editor))} />
      <TableBar editor={editor} />
      {linking && <LinkEditor editor={editor} state={linking} onClose={() => setLinking(null)} />}
    </div>
  );
}

// The / menu (VE-09).

interface SlashState {
  query: string;
  from: number;
  to: number;
  index: number;
}

interface SlashItem {
  id: string;
  icon: string;
  label: string;
  run: (e: Editor) => void;
}

const slashMenu: Omit<SlashItem, "label">[] = [
  { id: "h1", icon: mdiFormatHeader1, run: (e) => e.chain().focus().setNode("heading", { level: 1 }).run() },
  { id: "h2", icon: mdiFormatHeader2, run: (e) => e.chain().focus().setNode("heading", { level: 2 }).run() },
  { id: "h3", icon: mdiFormatHeader3, run: (e) => e.chain().focus().setNode("heading", { level: 3 }).run() },
  { id: "bullets", icon: mdiFormatListBulleted, run: (e) => e.chain().focus().toggleBulletList().run() },
  { id: "numbers", icon: mdiFormatListNumbered, run: (e) => e.chain().focus().toggleOrderedList().run() },
  { id: "tasks", icon: mdiCheckboxMarkedOutline, run: (e) => e.chain().focus().toggleTaskList().run() },
  { id: "quote", icon: mdiFormatQuoteClose, run: (e) => e.chain().focus().toggleBlockquote().run() },
  { id: "table", icon: mdiTable, run: (e) => e.chain().focus().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run() },
  { id: "code", icon: mdiCodeBraces, run: (e) => e.chain().focus().setNode("codeBlock").run() },
  {
    id: "diagram",
    icon: mdiSitemapOutline,
    run: (e) => e.chain().focus().setNode("codeBlock", { language: "mermaid" }).insertContent("flowchart LR\n  A --> B").run(),
  },
  { id: "rule", icon: mdiMinus, run: (e) => e.chain().focus().setHorizontalRule().run() },
];

/** The / menu's items for what's typed after the /. */
export function slashItems(query: string): SlashItem[] {
  const q = query.toLowerCase();
  return slashMenu.map((i) => ({ ...i, label: t(`desktop.visual.block.${i.id}`) })).filter((i) => !q || i.label.toLowerCase().includes(q) || i.id.includes(q));
}

// The / menu is open when an empty paragraph's text is a / and a word,
// the cursor at its end.
function slashAt(editor: Editor): SlashState | null {
  const { selection } = editor.state;
  if (!selection.empty) return null;
  const $from = selection.$from;
  if ($from.parent.type.name !== "paragraph") return null;
  const m = /^\/([\p{L}\p{N}-]*)$/u.exec($from.parent.textContent);
  if (!m || $from.parentOffset !== $from.parent.content.size) return null;
  const from = $from.start();
  return { query: m[1], from, to: from + $from.parent.content.size, index: 0 };
}

function runSlash(editor: Editor, s: SlashState, item: SlashItem) {
  editor.chain().focus().deleteRange({ from: s.from, to: s.to }).run();
  item.run(editor);
}

function SlashMenu({ editor, state, onPick }: { editor: Editor; state: SlashState; onPick: (item: SlashItem) => void }) {
  const items = slashItems(state.query);
  const at = editor.view.coordsAtPos(state.from);
  if (items.length === 0) return null;
  return (
    <Floating top={at.bottom + 6} left={at.left} className="visual-slash">
      {items.map((item, i) => (
        <button
          key={item.id}
          type="button"
          role="menuitem"
          className={`menu-item ${i === state.index ? "on" : ""}`}
          onMouseDown={(e) => {
            e.preventDefault(); // the document keeps the focus
            onPick(item);
          }}
        >
          <Icon path={item.icon} size="sm" />
          <span>{item.label}</span>
        </button>
      ))}
    </Floating>
  );
}

// Floats over the window at a point, in the app's menu style; the focus
// stays in the document.
function Floating({ top, left, className, children }: { top: number; left: number; className: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const x = Math.min(0, window.innerWidth - 8 - r.right);
    const y = r.bottom > window.innerHeight - 8 ? -(r.height + 40) : 0;
    el.style.transform = `translate(${x}px, ${y}px)`;
  });
  return createPortal(
    <div ref={ref} className={`menu floating ${className}`} role="menu" style={{ top, left }} onMouseDown={(e) => e.preventDefault()}>
      {children}
    </div>,
    document.body,
  );
}

// The selection bar (VE-09): marks, a link and the heading level.

function useEditorTick(editor: Editor) {
  const [, setTick] = useState(0);
  useEffect(() => {
    const f = () => setTick((n) => n + 1);
    editor.on("transaction", f);
    editor.on("blur", f);
    return () => {
      editor.off("transaction", f);
      editor.off("blur", f);
    };
  }, [editor]);
}

function SelectionBar({ editor, onLink }: { editor: Editor; onLink: () => void }) {
  useEditorTick(editor);
  const { selection } = editor.state;
  if (selection.empty || !editor.isFocused || editor.isActive("codeBlock") || selection instanceof NodeSelection) return null;
  const at = editor.view.coordsAtPos(selection.from);
  // Above the selection, or below it when the pane's top is in the way.
  const paneTop = editor.view.dom.closest(".preview")?.getBoundingClientRect().top ?? 0;
  const top = at.top - 46 < paneTop + 4 ? editor.view.coordsAtPos(selection.to).bottom + 8 : at.top - 46;
  const mark = (name: string, icon: string, label: string, toggle: () => boolean) => (
    <IconButton key={name} small icon={icon} label={label} selected={editor.isActive(name)} onMouseDown={(e) => e.preventDefault()} onClick={() => toggle()} />
  );
  return (
    <Floating top={top} left={at.left} className="visual-bar">
      {mark("bold", mdiFormatBold, t("desktop.visual.bold"), () => editor.chain().focus().toggleBold().run())}
      {mark("italic", mdiFormatItalic, t("desktop.visual.italic"), () => editor.chain().focus().toggleItalic().run())}
      {mark("code", mdiCodeTags, t("desktop.visual.code"), () => editor.chain().focus().toggleCode().run())}
      {mark("strike", mdiFormatStrikethroughVariant, t("desktop.visual.strike"), () => editor.chain().focus().toggleStrike().run())}
      <IconButton small icon={mdiLinkVariant} label={t("desktop.visual.link")} selected={editor.isActive("link")} onMouseDown={(e) => e.preventDefault()} onClick={onLink} />
      <span className="visual-bar-gap" />
      {[1, 2, 3].map((level) => (
        <IconButton
          key={level}
          small
          icon={[mdiFormatHeader1, mdiFormatHeader2, mdiFormatHeader3][level - 1]}
          label={t("desktop.visual.heading", { level })}
          selected={editor.isActive("heading", { level })}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => editor.chain().focus().toggleHeading({ level: level as 1 | 2 | 3 }).run()}
        />
      ))}
    </Floating>
  );
}

// The table bar (VE-12): rows and columns in and out, the table gone.

function TableBar({ editor }: { editor: Editor }) {
  useEditorTick(editor);
  if (!editor.isFocused || !editor.isActive("table")) return null;
  const $pos = editor.state.selection.$from;
  let depth = $pos.depth;
  while (depth > 0 && $pos.node(depth).type.name !== "table") depth--;
  const dom = editor.view.nodeDOM($pos.before(depth)) as HTMLElement | null;
  const box = dom?.getBoundingClientRect();
  if (!box) return null;
  const act = (icon: string, key: string, run: () => boolean) => <IconButton key={key} small icon={icon} label={t(`desktop.visual.table.${key}`)} onMouseDown={(e) => e.preventDefault()} onClick={() => run()} />;
  return (
    <Floating top={box.top - 44} left={box.left} className="visual-bar visual-table-bar">
      {act(mdiTableRowPlusAfter, "add_row", () => editor.chain().focus().addRowAfter().run())}
      {act(mdiTableColumnPlusAfter, "add_column", () => editor.chain().focus().addColumnAfter().run())}
      {act(mdiTableRowRemove, "delete_row", () => editor.chain().focus().deleteRow().run())}
      {act(mdiTableColumnRemove, "delete_column", () => editor.chain().focus().deleteColumn().run())}
      {act(mdiTableRemove, "delete_table", () => editor.chain().focus().deleteTable().run())}
    </Floating>
  );
}

// The link editor (VE-10): its address, applied to the selection or the
// link the cursor is in; removed with Remove.

interface LinkState {
  href: string;
  from: number;
  to: number;
}

function linkAt(editor: Editor): LinkState {
  editor.chain().extendMarkRange("link").run();
  const { from, to } = editor.state.selection;
  return { href: (editor.getAttributes("link").href as string) ?? "", from, to };
}

function LinkEditor({ editor, state, onClose }: { editor: Editor; state: LinkState; onClose: () => void }) {
  const [href, setHref] = useState(state.href);
  const at = editor.view.coordsAtPos(state.from);
  const apply = () => {
    const chain = editor.chain().focus().setTextSelection({ from: state.from, to: state.to });
    if (href.trim()) {
      if (state.from === state.to) chain.insertContent({ type: "text", text: href.trim(), marks: [{ type: "link", attrs: { href: href.trim() } }] }).run();
      else chain.setLink({ href: href.trim() }).run();
    } else chain.unsetLink().run();
    onClose();
  };
  return createPortal(
    <div className="menu floating visual-link" style={{ top: at.bottom + 6, left: at.left }}>
      <input
        className="input"
        autoFocus
        value={href}
        placeholder={t("desktop.visual.link_placeholder")}
        aria-label={t("desktop.visual.link")}
        onChange={(e) => setHref(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") apply();
          if (e.key === "Escape") {
            onClose();
            editor.commands.focus();
          }
        }}
      />
      <IconButton small icon={mdiLinkVariant} label={t("desktop.visual.link_apply")} onClick={apply} />
      {state.href && (
        <IconButton
          small
          icon={mdiLinkVariantOff}
          label={t("desktop.visual.link_remove")}
          onClick={() => {
            editor.chain().focus().setTextSelection({ from: state.from, to: state.to }).unsetLink().run();
            onClose();
          }}
        />
      )}
    </div>,
    document.body,
  );
}
