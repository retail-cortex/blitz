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

// A workspace's settings as the page shows them: the settings, whether
// the model works, and the project settings, with the question about the
// project's settings asked once as it opens. The desktop app's workspace
// and the editor's panel share it.
import { useCallback, useEffect, useRef, useState } from "react";
import { workspaces } from "./api";
import { message, reason } from "./errors";
import { configChangedEvent, type ConfigChangedDetail } from "./events";
import type { GetSettingsResponse, ProjectSettings } from "./gen/blitz/v1/workspace_pb";
import { needsDecision } from "./project";

/** A workspace's settings, and the project-settings review to show. */
export function useWorkspaceSettings(dir: string) {
  const [settings, setSettings] = useState<GetSettingsResponse>();
  const [modelProblem, setModelProblem] = useState("");
  const [settingsError, setSettingsError] = useState("");
  // Its reason (SANDBOX_UNAVAILABLE: the workspace didn't open for want of the sandbox).
  const [settingsReason, setSettingsReason] = useState("");
  const [project, setProject] = useState<ProjectSettings>();
  const [reviewing, setReviewing] = useState(false);
  // The project settings' content asked about as the workspace opened:
  // asked once, not again at each refresh.
  const asked = useRef("");

  const refreshSettings = useCallback(async () => {
    try {
      const [s, m, p] = await Promise.all([
        workspaces.getSettings({ workspace: dir }),
        workspaces.getModel({ workspace: dir }),
        workspaces.getProjectSettings({ workspace: dir }).then(
          (r) => r.settings,
          () => undefined, // an older service
        ),
      ]);
      setSettings(s);
      setModelProblem(m.unavailable);
      setProject(p);
      setSettingsError("");
      setSettingsReason("");
      if (p && needsDecision(p) && asked.current !== p.hash) {
        asked.current = p.hash;
        setReviewing(true);
      }
    } catch (e) {
      setSettingsError(message(e));
      setSettingsReason(reason(e));
    }
  }, [dir]);
  useEffect(() => {
    refreshSettings();
  }, [refreshSettings]);

  // Keys and providers set in the settings: is the model usable now?
  useEffect(() => {
    const f = (e: Event) => {
      const d = (e as CustomEvent<ConfigChangedDetail>).detail;
      if (d.dir === "" || d.dir === dir) refreshSettings();
    };
    window.addEventListener(configChangedEvent, f);
    return () => window.removeEventListener(configChangedEvent, f);
  }, [dir, refreshSettings]);

  // A model unavailable for want of a sign-in made outside the app (gcloud,
  // ant auth login) works once it's made: coming back to the window asks
  // again, and the service tries the model again when asked.
  useEffect(() => {
    if (!modelProblem) return;
    const f = () => refreshSettings();
    window.addEventListener("focus", f);
    return () => window.removeEventListener("focus", f);
  }, [modelProblem, refreshSettings]);

  return { settings, modelProblem, settingsError, settingsReason, project, reviewing, setReviewing, refreshSettings };
}

/**
 * Calls f when settings change for dir (or the global ones, which reach
 * every workspace): edited here, or on disk by anyone (the service's
 * settings_changed).
 */
export function useOnConfigChanged(dir: string, f: () => void) {
  const latest = useRef(f);
  latest.current = f;
  useEffect(() => {
    const on = (e: Event) => {
      const d = (e as CustomEvent<ConfigChangedDetail>).detail;
      if (d.dir === "" || dir === "" || d.dir === dir) latest.current();
    };
    window.addEventListener(configChangedEvent, on);
    return () => window.removeEventListener(configChangedEvent, on);
  }, [dir]);
}
