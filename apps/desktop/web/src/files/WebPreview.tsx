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

import { useEffect, useRef, useState } from "react";
import { mdiArrowLeft, mdiArrowRight, mdiClose, mdiCameraOutline, mdiConsoleLine, mdiContentSaveOutline, mdiMessagePlusOutline, mdiOpenInNew, mdiRefresh, mdiRobotOutline, mdiSelectionDrag, mdiWeb } from "@mdi/js";
import { agentBrowser, openURL, previewBounds, previewCapture, previewConsole, previewContent, previewNavigate, previewOpen, previewVisible } from "../desktop";
import { message } from "../errors";
import { addWeb } from "../events";
import { t } from "../i18n";
import { Button, Icon, IconButton, Menu, useSnackbar } from "../ui/controls";
import { SaveWebDialog } from "./SaveWebDialog";
import type { EditorModel, Tab } from "./useEditor";
import { consoleBlock, covered, overlays, pageBlock, webId, webTabName } from "./webTabs";

// The previews this page has opened in the window (a tab shown again
// keeps its page).
const opened = new Set<string>();

/** Takes the window's view id as opened: the agent's (AB-03), which it opened itself. */
export const adoptPreview = (id: string) => opened.add(id);

/**
 * A web page in the editor (spec_electron_038 EL-100): its toolbar (back,
 * forward, reload, the address, Add to chat) over the place the window's
 * view of the page fills. The view is drawn above the page, so it's
 * hidden while a menu, a dialog or a drawer overlaps it, and while the tab
 * isn't shown.
 */
export function WebPreview({ tab, model }: { tab: Tab & { web: NonNullable<Tab["web"]> }; model: EditorModel }) {
  const id = webId(tab.path);
  const web = tab.web;
  const place = useRef<HTMLDivElement>(null);
  const [address, setAddress] = useState(web.url);
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState("");
  const [started, setStarted] = useState(opened.has(id));
  const [saving, setSaving] = useState(false);
  const snack = useSnackbar();

  // The address follows the page, unless it's being typed.
  useEffect(() => {
    if (!editing) setAddress(web.url);
  }, [web.url, editing]);

  // An address opens in the tab's view (made the first time); the
  // toolbar's buttons move it.
  const go = async (to: string) => {
    setError("");
    try {
      const url = await previewOpen(id, to);
      opened.add(id);
      setStarted(true);
      model.setWeb(tab.path, { url, loading: true });
    } catch (e) {
      setError(message(e));
    }
  };
  const step = (s: "back" | "forward" | "reload" | "stop") => void previewNavigate(id, s).catch((e) => setError(message(e)));
  // A tab opened with an address loads it; one without asks for one.
  useEffect(() => {
    if (!opened.has(id) && web.url) void go(web.url);
    // go reads the tab; this runs once a tab.
  }, [id]);

  // The view goes where the placeholder is, shown while nothing covers it.
  useEffect(() => {
    if (!started) return;
    const el = place.current;
    if (!el) return;
    let frame = 0;
    let last = "";
    const measure = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const r = el.getBoundingClientRect();
        const others = [...document.querySelectorAll<HTMLElement>(overlays)].map((o) => o.getBoundingClientRect());
        const show = el.offsetParent !== null && r.width > 0 && r.height > 0 && !covered(r, others);
        const now = `${show}:${Math.round(r.left)},${Math.round(r.top)},${Math.round(r.width)},${Math.round(r.height)}`;
        if (now === last) return;
        last = now;
        if (show) void previewBounds(id, { x: r.left, y: r.top, width: r.width, height: r.height });
        void previewVisible(id, show);
      });
    };
    measure();
    const sized = new ResizeObserver(measure);
    sized.observe(el);
    const changed = new MutationObserver(measure);
    changed.observe(document.body, { subtree: true, childList: true, attributes: true, attributeFilter: ["hidden", "class", "style"] });
    window.addEventListener("resize", measure);
    document.addEventListener("transitionend", measure, true);
    return () => {
      cancelAnimationFrame(frame);
      sized.disconnect();
      changed.disconnect();
      window.removeEventListener("resize", measure);
      document.removeEventListener("transitionend", measure, true);
      void previewVisible(id, false);
    };
  }, [id, started]);

  // Add to chat: the page, the selection, a screenshot, the console's errors.
  const add = async (what: "page" | "selection" | "screenshot" | "console") => {
    try {
      if (what === "screenshot") {
        const data = await previewCapture(id);
        addWeb({ dir: model.dir, image: { name: `${webTabName(web.url, web.title)}.png`.replace(/[/\\:]/g, "-"), data } });
      } else if (what === "console") {
        const lines = await previewConsole(id);
        if (!lines.length) return snack(t("desktop.web.no_errors"));
        addWeb({ dir: model.dir, block: consoleBlock(web.url, web.title, lines) });
      } else {
        const c = await previewContent(id);
        if (what === "selection" && !c.selection) return snack(t("desktop.web.no_selection"));
        addWeb({ dir: model.dir, block: pageBlock(c, what) });
      }
      // The chip in the composer says it was added (a snackbar would hide the page).
    } catch (e) {
      snack(t("desktop.web.add_failed", { error: message(e) }), { error: true });
    }
  };

  return (
    <>
      <div className="editor-head web-bar">
        <IconButton icon={mdiArrowLeft} label={t("desktop.web.back")} small disabled={!web.back} onClick={() => step("back")} />
        <IconButton icon={mdiArrowRight} label={t("desktop.web.forward")} small disabled={!web.forward} onClick={() => step("forward")} />
        {web.loading && started ? (
          <IconButton icon={mdiClose} label={t("desktop.web.stop")} small onClick={() => step("stop")} />
        ) : (
          <IconButton icon={mdiRefresh} label={t("desktop.web.reload")} small disabled={!started} onClick={() => step("reload")} />
        )}
        <form
          className="web-address-form"
          onSubmit={(e) => {
            e.preventDefault();
            setEditing(false);
            if (address.trim()) void go(address.trim());
          }}
        >
          <input
            className="web-address"
            value={address}
            placeholder={t("desktop.web.address")}
            aria-label={t("desktop.web.address")}
            spellCheck={false}
            autoFocus={!web.url}
            onFocus={(e) => (setEditing(true), e.currentTarget.select())}
            onBlur={() => setEditing(false)}
            onChange={(e) => setAddress(e.target.value)}
          />
        </form>
        <IconButton icon={mdiContentSaveOutline} label={t("desktop.web.save.title")} small disabled={!started || !web.url} onClick={() => setSaving(true)} />
        <IconButton icon={mdiOpenInNew} label={t("desktop.web.open_browser")} small disabled={!web.url} onClick={() => void openURL(web.url).catch((e) => snack(message(e), { error: true }))} />
        <Menu
          trigger={(props) => (
            <Button {...props} small icon={mdiMessagePlusOutline} disabled={!started}>
              {t("desktop.web.add_to_chat")}
            </Button>
          )}
          placement="down end"
          items={[
            { label: t("desktop.web.add_page"), icon: mdiWeb, onSelect: () => void add("page") },
            { label: t("desktop.web.add_selection"), icon: mdiSelectionDrag, onSelect: () => void add("selection") },
            { label: t("desktop.web.add_screenshot"), icon: mdiCameraOutline, onSelect: () => void add("screenshot") },
            { label: t("desktop.web.add_console"), icon: mdiConsoleLine, onSelect: () => void add("console") },
          ]}
        />
      </div>
      {web.agent && (
        // The agent drives this page (spec_agent_browser_050 AB-03): the
        // user can take it over, or stop the agent's browsing.
        <div className="card row t-body-sm web-agent-bar" role="status">
          <Icon path={mdiRobotOutline} size="sm" />
          <span className="spacer">{t("desktop.web.agent_browsing")}</span>
          <Button small onClick={() => void agentBrowser.takeOver(id).then(() => model.setWeb(tab.path, { agent: false }))}>
            {t("desktop.web.take_over")}
          </Button>
          <Button small onClick={() => void agentBrowser.close(id).then(() => model.close(tab.path))}>
            {t("desktop.web.stop_agent")}
          </Button>
        </div>
      )}
      <div className="editor-body">
        {error && (
          <p className="editor-note error-text" role="alert">
            {error}
          </p>
        )}
        {!started && !error && (
          <p className="editor-note muted">
            <Icon path={mdiWeb} size="sm" /> {t("desktop.web.empty")}
          </p>
        )}
        <div className="web-view" ref={place} hidden={!started} />
      </div>
      {saving && <SaveWebDialog dir={model.dir} id={id} url={web.url} title={web.title} onClose={() => setSaving(false)} />}
    </>
  );
}
