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

import { config, workspaces } from "../api";
import type { Connector, ConnectorCategory, ConnectorKind, ConnectorQuery, ConnectorTable, ConnectorTool } from "../gen/blitz/v1/workspace_pb";
import type { connectionFromForm } from "./connectorForm";

/** A connection as written in the form, for the service. */
export type Written = ReturnType<typeof connectionFromForm>;

/**
 * Where a pane's connections are kept and changed (spec_connectors_047
 * CON-18): a workspace's (Studio, which lists the global ones too but
 * never changes them), or the global ones (Settings, with no workspace).
 */
export type ConnectorApi = {
  global: boolean;
  list: () => Promise<{ categories: ConnectorCategory[]; kinds: ConnectorKind[]; connections: Connector[] }>;
  save: (r: Written & { previousName: string }) => Promise<{ connection?: Connector; problems: string[] }>;
  remove: (c: Connector) => Promise<unknown>;
  test: (r: Written) => Promise<{ tools: ConnectorTool[]; problem: string }>;
  tryTool: (r: Written & { tool: string; argsJson: string }) => Promise<{ output: string; isError: boolean; problem: string }>;
  readSchema: (r: Written) => Promise<{ tables: ConnectorTable[]; problem: string }>;
  draft: (r: Written & { request: string }) => Promise<{ query?: ConnectorQuery; notes: string[]; tables: number; problem: string }>;
};

/** A workspace's connections, through WorkspaceService. */
export const workspaceApi = (workspace: string): ConnectorApi => ({
  global: false,
  list: () => workspaces.listConnectors({ workspace }),
  save: (r) => workspaces.saveConnector({ workspace, ...r }),
  remove: (c) => workspaces.deleteConnector({ workspace, category: c.category, scope: c.scope, name: c.name }),
  test: (r) => workspaces.testConnector({ workspace, ...r }),
  tryTool: (r) => workspaces.tryConnectorTool({ workspace, ...r }),
  readSchema: (r) => workspaces.readConnectorSchema({ workspace, ...r }),
  draft: (r) => workspaces.draftConnectorQuery({ workspace, ...r }),
});

/** The global connections, through ConfigService. */
export const globalApi: ConnectorApi = {
  global: true,
  list: () => config.listGlobalConnectors({}),
  save: (r) => config.saveGlobalConnector(r),
  remove: (c) => config.deleteGlobalConnector({ category: c.category, name: c.name }),
  test: (r) => config.testGlobalConnector(r),
  tryTool: (r) => config.tryGlobalConnectorTool(r),
  readSchema: (r) => config.readGlobalConnectorSchema(r),
  draft: (r) => config.draftGlobalConnectorQuery(r),
};

/** The agents' name for a connector tool: its category's prefix, then __. */
export const agentTool = (prefix: string, tool: string) => `${prefix}__${tool}`;
