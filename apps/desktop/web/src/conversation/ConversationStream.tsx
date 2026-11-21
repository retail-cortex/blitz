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

import { showView } from "../events";
import { ModelFile } from "../ModelFile";
import { ShellCard } from "../ShellCard";
import { EntryView, ToolGroup } from "./MessageEntries";
import { JobCard, ReflectCard, TaskCard } from "./StatusCards";
import { groupTools, type Entry } from "../turns";

/** Renders the stream of entries and tool results in the conversation. */
export function ConversationStream({
  shown,
  running,
  turnStart,
  dir,
  sessionId,
  answers,
  stopShell,
  rewind,
  setDraft,
}: {
  shown: Entry[];
  running: boolean;
  turnStart: number;
  dir: string;
  sessionId: string;
  answers: Map<number, string>;
  stopShell: (id: string) => void;
  rewind: (index: number, mode: string, force?: boolean) => void;
  setDraft: (d: string) => void;
}) {
  return (
    <>
      {groupTools(shown).map((g) =>
        g.kind === "tools" ? (
          <div key={g.at}>
            <ToolGroup tools={g.tools} live={running && g.at >= turnStart} />
            {g.tools.map((x) =>
              x.name === "invoke_agent" && typeof x.result?.task_id === "string" ? (
                <TaskCard key={x.result.task_id} dir={dir} sessionId={sessionId} id={x.result.task_id} />
              ) : x.name === "create_job" && typeof x.result?.name === "string" && x.result.paused === true ? (
                <JobCard key={`job-${x.result.name}`} dir={dir} name={x.result.name} />
              ) : x.name === "generate_image" && typeof x.result?.image_uri === "string" && typeof x.result.path === "string" ? (
                <ModelFile
                  key={x.result.image_uri}
                  dir={dir}
                  entry={{ kind: "file", uri: x.result.image_uri, name: x.result.path, mime: String(x.result.mime_type ?? "image/png"), size: Number(x.result.bytes ?? 0) }}
                  onPictures={typeof x.result.picture_session === "string" ? () => showView({ dir, view: "pictures", picture: { session: String(x.result?.picture_session) } }) : undefined}
                />
              ) : null,
            )}
          </div>
        ) : g.entry.kind === "reflect" ? (
          <ReflectCard key={g.at} dir={dir} patterns={g.entry.patterns} />
        ) : g.entry.kind === "file" ? (
          <ModelFile key={g.at} dir={dir} entry={g.entry} />
        ) : g.entry.kind === "shell" ? (
          <ShellCard key={g.entry.id} entry={g.entry} onStop={stopShell} />
        ) : (
          <EntryView key={g.at} entry={g.entry} turn={answers.get(g.at)} running={running} onRewind={rewind} onEdit={setDraft} />
        ),
      )}
    </>
  );
}
