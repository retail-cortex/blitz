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

import { AgentEditor } from "./AgentEditor";
import { AgentScope } from "./gen/blitz/v1/workspace_pb";

/**
 * A workspace's own agents (.agents/agents): the Agents view in the
 * middle of the window. The user's agents, for every workspace, are in
 * Settings › Agents.
 */
export function Agents({ dir }: { dir: string }) {
  return <AgentEditor workspace={dir} scope={AgentScope.WORKSPACE} />;
}
