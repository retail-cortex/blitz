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

// A fake Blitz service for working on the page without one (?fake in a
// development build). It keeps a little state in memory and scripts turns
// so every state of the conversation can be seen: streamed Markdown,
// thinking, tool calls, a task list, an approval and a plan review.
import { createRouterTransport } from "@connectrpc/connect";
import { setTransport } from "../api";
import { ConfigService } from "../gen/blitz/v1/config_pb";
import { FileService } from "../gen/blitz/v1/file_pb";
import { SessionService } from "../gen/blitz/v1/session_pb";
import { TerminalService } from "../gen/blitz/v1/terminal_pb";
import { VoiceService } from "../gen/blitz/v1/voice_pb";
import { WorkerService } from "../gen/blitz/v1/worker_pb";
import { WorkspaceService } from "../gen/blitz/v1/workspace_pb";
import { fakePersonalities, fakePersonalityIn } from "./fakeConfigData";
import { fakeConfigService } from "./fakeConfigService";
import { fakeFileService } from "./fakeFileService";
import { keepalivesFor } from "./fakeHelpers";
import { fakeSessionService } from "./fakeSessionService";
import { fakeTerminals } from "./fakeTerminal";
import { fakeVoice } from "./fakeVoice";
import { fakeWorkerService } from "./fakeWorkerService";
import { fakeWorkspaceService } from "./fakeWorkspaceService";

/** Replaces the service with the in-page fake (development: ?fake). */
export function installFake() {
  setTransport(
    createRouterTransport((router) => {
      // Every server stream with the page server's keepalives in it.
      const service = keepalivesFor(router);
      service(SessionService, fakeSessionService());
      service(WorkspaceService, fakeWorkspaceService());
      service(ConfigService, fakeConfigService());
      service(TerminalService, fakeTerminals());
      service(
        VoiceService,
        fakeVoice((workspace) => {
          const p = fakePersonalities().find((x) => x.name === fakePersonalityIn(workspace));
          return { voice: p?.voice ?? "", locale: p?.locale ?? "", speed: p?.speed ?? 0, pitch: p?.pitch ?? 0 };
        }),
      );
      service(FileService, fakeFileService());
      service(WorkerService, fakeWorkerService());
    }),
  );
}
