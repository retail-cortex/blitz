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
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode } from "react";
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
  mdiImageOutline,
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
import { Button, ContextMenu, Icon, IconButton, useSnackbar, type MenuEntry } from "../../ui/controls";
import type { LanguageHooks } from "../language";
import { CodeBlockView, type BlockContext } from "./codeBlock";
import { fingerprints, parseMarkdown, serializeMarkdown, type Parsed } from "./markdown";
import { sourceAttr, visualExtensions } from "./schema";
import "./visual.css";

/** How long typing waits before the Markdown is written (VE-06). */
const writeDelay = 150;

// The extensions with their views: code and raw blocks as CodeMirror,
// images as Preview's boxes (never loaded, DSK-24).
function editorExtensions(context?: BlockContext) {
  return visualExtensions().map((ext) => {
    switch (ext.name) {
      case "codeBlock":
        return ext.extend({ addNodeView: () => ({ node, view, getPos }: NodeViewRendererProps) => new CodeBlockView(node, view, getPos, "code", context) });
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
  /** The language servers' hooks, for code blocks (none: no servers). */
  language?: LanguageHooks;
}

/** A Markdown file, edited in its rendered form. */
export function VisualEditor({ dir, path, text, onChange, onSave, language }: Props) {
  const snack = useSnackbar();
  const parsed = useRef<Parsed | null>(null);
  const prints = useRef(new Map<string, string>());
  const written = useRef<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const change = useRef(onChange);
  change.current = onChange;
  const save = useRef(onSave);
  save.current = onSave;
  // The editor is made once: a block's hooks are read when they're called.
  const hooks = useRef(language);
  hooks.current = language;
  const hasServers = !!language;
  const extensions = useMemo(
    () =>
      editorExtensions(
        hasServers
          ? {
              dir,
              path,
              hooks: {
                open: (p, line, column) => hooks.current?.open(p, line, column),
                outside: (loc) => hooks.current?.outside(loc),
                markdown: (md) => hooks.current?.markdown(md) ?? { dom: document.createElement("div"), destroy: () => {} },
              },
            }
          : undefined,
      ),
    [dir, path, hasServers],
  );
  const [slash, setSlash] = useState<SlashState | null>(null);
  const slashRef = useRef<SlashState | null>(null);
  slashRef.current = slash;
  const [linking, setLinking] = useState<LinkState | null>(null);
  const [imaging, setImaging] = useState<ImageState | null>(null);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);

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
  // TipTap may replace its editor (React mounts twice in development), so
  // the text is loaded into each editor it hands over.
  const loadedInto = useRef<Editor | null>(null);
  useLayoutEffect(() => {
    if (!editor || editor.isDestroyed) return;
    if (editor === loadedInto.current && text === written.current) return;
    loadedInto.current = editor;
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

  // ⌘S: written, then saved; the code blocks ask for it the same way (an
  // event that bubbles to the wrapper: the editor's view mounts after this).
  const wrapper = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const dom = wrapper.current;
    if (!dom || !editor || editor.isDestroyed) return;
    const onSave = () => {
      write(editor);
      save.current();
    };
    // ⌘S from the toolbar or a menu, or with the focus nowhere (a menu
    // just closed), saves too.
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.shiftKey || e.altKey || e.key.toLowerCase() !== "s") return;
      const at = document.activeElement;
      if (!(at === document.body || (at && dom.contains(at))) || dom.offsetParent === null) return;
      e.preventDefault();
      e.stopPropagation();
      onSave();
    };
    dom.addEventListener("visual-save", onSave);
    window.addEventListener("keydown", onKey, true);
    return () => {
      dom.removeEventListener("visual-save", onSave);
      window.removeEventListener("keydown", onKey, true);
    };
  }, [editor, write]);

  // The / menu's keys, while it's open.
  const slashKeys = (event: KeyboardEvent): boolean => {
    if ((event.metaKey || event.ctrlKey) && !event.shiftKey && !event.altKey) {
      if (event.key === "k" && editor) {
        // The link, not the app's command palette (⌘K elsewhere).
        event.preventDefault();
        event.stopPropagation();
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

  if (!editor || editor.isDestroyed) return null;
  const open: Openers = { link: () => setLinking(linkAt(editor)), image: () => setImaging(imageAt(editor)) };
  return (
    <div
      ref={wrapper}
      className="visual-editor"
      onContextMenu={(e) => {
        if ((e.target as HTMLElement).closest(".visual-code, .visual-toolbar")) return; // CodeMirror's, and the browser's on buttons
        e.preventDefault();
        setMenu({ x: e.clientX, y: e.clientY });
      }}
    >
      <div className="visual-scroll">
        <EditorContent editor={editor} />
      </div>
      <Toolbar editor={editor} open={open} />
      {slash && <SlashMenu editor={editor} state={slash} onPick={(item) => runSlash(editor, slash, item)} />}
      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          items={contextItems(editor, open)}
          onClose={() => {
            setMenu(null);
            editor.commands.focus(); // back to the document, for the next key
          }}
        />
      )}
      {linking && <LinkEditor editor={editor} state={linking} onClose={() => setLinking(null)} />}
      {imaging && <ImageEditor editor={editor} state={imaging} onClose={() => setImaging(null)} />}
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

// The formatting actions (VE-09): the toolbar above the document and the
// right-click menu show the same ones, in the same groups.

function useEditorTick(editor: Editor) {
  const [, setTick] = useState(0);
  useEffect(() => {
    const f = () => setTick((n) => n + 1);
    editor.on("transaction", f);
    editor.on("focus", f);
    editor.on("blur", f);
    return () => {
      editor.off("transaction", f);
      editor.off("focus", f);
      editor.off("blur", f);
    };
  }, [editor]);
}

/** The link and image editors the actions open. */
interface Openers {
  link: () => void;
  image: () => void;
}

interface Action {
  id: string;
  icon: string;
  label: string;
  active?: boolean;
  disabled?: boolean;
  run: () => void;
}

/** The actions, in groups, for the editor as it is now. */
export function actionGroups(editor: Editor, open: Openers): Action[][] {
  const chain = () => editor.chain().focus();
  const inCode = editor.isActive("codeBlock") || editor.isActive("rawBlock");
  const a = (id: string, icon: string, run: () => unknown, active?: boolean, label = t(`desktop.visual.action.${id}`)): Action => ({ id, icon, label, active, disabled: inCode && id !== "code_block", run: () => void run() });
  const groups: Action[][] = [
    [1, 2, 3].map((level) =>
      a(`h${level}`, [mdiFormatHeader1, mdiFormatHeader2, mdiFormatHeader3][level - 1], () => chain().toggleHeading({ level: level as 1 | 2 | 3 }).run(), editor.isActive("heading", { level }), t("desktop.visual.heading", { level })),
    ),
    [
      a("bold", mdiFormatBold, () => chain().toggleBold().run(), editor.isActive("bold")),
      a("italic", mdiFormatItalic, () => chain().toggleItalic().run(), editor.isActive("italic")),
      a("code", mdiCodeTags, () => chain().toggleCode().run(), editor.isActive("code")),
      a("strike", mdiFormatStrikethroughVariant, () => chain().toggleStrike().run(), editor.isActive("strike")),
    ],
    [
      a("bullets", mdiFormatListBulleted, () => chain().toggleBulletList().run(), editor.isActive("bulletList")),
      a("numbers", mdiFormatListNumbered, () => chain().toggleOrderedList().run(), editor.isActive("orderedList")),
      a("tasks", mdiCheckboxMarkedOutline, () => chain().toggleTaskList().run(), editor.isActive("taskList")),
    ],
    [
      a("quote", mdiFormatQuoteClose, () => chain().toggleBlockquote().run(), editor.isActive("blockquote")),
      a("code_block", mdiCodeBraces, () => chain().toggleCodeBlock().run(), editor.isActive("codeBlock")),
      a("diagram", mdiSitemapOutline, () => chain().setNode("codeBlock", { language: "mermaid" }).insertContent("flowchart LR\n  A --> B").run()),
      a("rule", mdiMinus, () => chain().setHorizontalRule().run()),
    ],
    [
      a("link", mdiLinkVariant, open.link, editor.isActive("link")),
      a("image", mdiImageOutline, open.image, editor.isActive("image")),
      a("table", mdiTable, () => chain().insertTable({ rows: 3, cols: 3, withHeaderRow: true }).run(), editor.isActive("table")),
    ],
  ];
  if (editor.isActive("table"))
    groups.push([
      a("add_row", mdiTableRowPlusAfter, () => chain().addRowAfter().run()),
      a("add_column", mdiTableColumnPlusAfter, () => chain().addColumnAfter().run()),
      a("delete_row", mdiTableRowRemove, () => chain().deleteRow().run()),
      a("delete_column", mdiTableColumnRemove, () => chain().deleteColumn().run()),
      a("delete_table", mdiTableRemove, () => chain().deleteTable().run()),
    ]);
  return groups;
}

// The toolbar: always there, docked at the panel's foot under the document.
function Toolbar({ editor, open }: { editor: Editor; open: Openers }) {
  useEditorTick(editor);
  const groups = actionGroups(editor, open);
  const bar = useRef<HTMLDivElement>(null);
  // Which ends have more to scroll to, for the fades that say so.
  const [more, setMore] = useState({ start: false, end: false });
  const measure = useCallback(() => {
    const el = bar.current;
    if (!el) return;
    const next = { start: el.scrollLeft > 1, end: el.scrollLeft + el.clientWidth < el.scrollWidth - 1 };
    setMore((m) => (m.start === next.start && m.end === next.end ? m : next));
  }, []);
  useEffect(() => {
    const el = bar.current;
    if (!el) return;
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);
  // The table's actions come into view when the cursor enters a table.
  const inTable = groups.length > 5;
  useEffect(() => {
    if (inTable) bar.current?.querySelector(".visual-toolbar-table")?.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "smooth" });
    measure();
  }, [inTable, measure]);
  return (
    <div
      ref={bar}
      onScroll={measure}
      className={`visual-toolbar${more.start ? " more-start" : ""}${more.end ? " more-end" : ""}`}
      role="toolbar"
      aria-label={t("desktop.visual.toolbar")}
      // A mouse wheel scrolls the one row sideways (trackpads do already).
      onWheel={(e) => {
        if (e.deltaX === 0 && e.deltaY !== 0) e.currentTarget.scrollLeft += e.deltaY;
      }}
    >
      {groups.map((g, i) => (
        <div key={i} className={i === 5 ? "visual-toolbar-group visual-toolbar-table" : "visual-toolbar-group"}>
          {g.map((x) => (
            <IconButton key={x.id} small icon={x.icon} label={x.label} selected={x.active} disabled={x.disabled} onMouseDown={(e) => e.preventDefault()} onClick={x.run} />
          ))}
        </div>
      ))}
    </div>
  );
}

// The right-click menu: the actions that make sense where it was opened,
// the table's first when it's in one.
function contextItems(editor: Editor, open: Openers): MenuEntry[] {
  const groups = actionGroups(editor, open);
  const pick = (g: Action[]) => g.map((x) => ({ label: x.label, icon: x.icon, on: x.active, disabled: x.disabled, onSelect: x.run }));
  const [headings, marks, lists, blocks, insert, table] = groups;
  const out: MenuEntry[] = [];
  if (table) out.push({ heading: t("desktop.visual.table_heading") }, ...pick(table), "divider");
  out.push(...pick(marks), "divider", ...pick(insert), "divider", ...pick(headings), "divider", ...pick(lists), "divider", ...pick(blocks));
  return out;
}

// The link and image editors' popover: at the cursor, inside the window
// (above the cursor when there's no room below), the focus in its field.
function Popover({ at, className, children }: { at: { top: number; bottom: number; left: number }; className: string; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const x = Math.min(0, window.innerWidth - 8 - r.right) + Math.max(0, 8 - r.left);
    const y = r.bottom > window.innerHeight - 8 ? at.top - 6 - r.height - r.top : 0;
    el.style.transform = `translate(${x}px, ${y}px)`;
  });
  return createPortal(
    <div ref={ref} className={`menu floating visual-popover ${className}`} style={{ top: at.bottom + 6, left: at.left }}>
      {children}
    </div>,
    document.body,
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
  return (
    <Popover at={at} className="visual-link">
      <input
        className="input"
        autoFocus
        value={href}
        placeholder={t("desktop.visual.link_placeholder")}
        aria-label={t("desktop.visual.link")}
        onChange={(e) => setHref(e.target.value)}
        onKeyDown={(e) => {
          // Consumed here: the focus goes back to the document, which would
          // take the same key (an Enter replacing the selection).
          if (e.key === "Enter" || e.key === "Escape") e.preventDefault();
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
    </Popover>
  );
}

// The image editor (VE-11): its address and description, for the picture
// selected or a new one at the cursor. Pictures show as boxes, never
// loaded (DSK-24).

interface ImageState {
  src: string;
  alt: string;
  /** The picture being edited, or null for a new one. */
  pos: number | null;
}

function imageAt(editor: Editor): ImageState {
  const { selection } = editor.state;
  if (selection instanceof NodeSelection && selection.node.type.name === "image") {
    return { src: selection.node.attrs.src as string, alt: (selection.node.attrs.alt as string) ?? "", pos: selection.from };
  }
  return { src: "", alt: "", pos: null };
}

function ImageEditor({ editor, state, onClose }: { editor: Editor; state: ImageState; onClose: () => void }) {
  const [src, setSrc] = useState(state.src);
  const [alt, setAlt] = useState(state.alt);
  const at = editor.view.coordsAtPos(state.pos ?? editor.state.selection.from);
  const apply = () => {
    if (!src.trim()) return onClose();
    const attrs = { src: src.trim(), alt: alt.trim(), title: null };
    if (state.pos !== null) editor.chain().focus().command(({ tr }) => (tr.setNodeMarkup(state.pos!, undefined, attrs), true)).run();
    else editor.chain().focus().insertContent({ type: "image", attrs }).run();
    onClose();
  };
  const keys = (e: ReactKeyboardEvent) => {
    if (e.key === "Enter" || e.key === "Escape") e.preventDefault(); // not the document's too
    if (e.key === "Enter") apply();
    if (e.key === "Escape") {
      onClose();
      editor.commands.focus();
    }
  };
  return (
    <Popover at={at} className="visual-image">
      <label className="t-label-sm muted" htmlFor="visual-image-src">
        {t("desktop.visual.image_address")}
      </label>
      <input id="visual-image-src" className="input" autoFocus value={src} placeholder={t("desktop.visual.image_placeholder")} onChange={(e) => setSrc(e.target.value)} onKeyDown={keys} />
      <label className="t-label-sm muted" htmlFor="visual-image-alt">
        {t("desktop.visual.image_alt")}
      </label>
      <input id="visual-image-alt" className="input" value={alt} onChange={(e) => setAlt(e.target.value)} onKeyDown={keys} />
      <div className="visual-popover-actions">
        <Button
          small
          onClick={() => {
            onClose();
            editor.commands.focus();
          }}
        >
          {t("desktop.cancel")}
        </Button>
        <Button small variant="filled" icon={mdiImageOutline} disabled={!src.trim()} onClick={apply}>
          {state.pos !== null ? t("desktop.visual.image_update") : t("desktop.visual.image_apply")}
        </Button>
      </div>
    </Popover>
  );
}
