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

// Include all changes (spec_files_029 FIL-59a): every change in the
// workspace staged for the next commit, new files included and what git
// ignores left out, from the Changes view, its menu and the Files tree.
import { files } from "./api";
import { GitAction } from "./gen/blitz/v1/file_pb";

/** Stages every change in the workspace dir (git add --all). */
export async function includeAll(dir: string): Promise<void> {
  await files.gitFileAction({ workspace: dir, path: "", action: GitAction.STAGE });
}
