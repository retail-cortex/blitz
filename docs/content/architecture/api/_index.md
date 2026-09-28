---
title: API reference
weight: 25
geekdocCollapseSection: true
---

The service's API, one page per proto file in `proto/blitz/v1`: each message, enum and service as a table and a Mermaid class diagram. The pages are generated from the protos by [proto-gen-md-diagrams](https://github.com/GoogleCloudPlatform/proto-gen-md-diagrams) when the site is built (`//docs:api`), so they can't drift from the API. [The service API](../service-api.md) explains the design.

| Proto | Service | |
|---|---|---|
| [`session.proto`](session.md) | `SessionService` | Sessions, running turns, steering, approvals and answers |
| [`turn.proto`](turn.md) | | A turn and the events it streams |
| [`workspace.proto`](workspace.md) | `WorkspaceService` | Agents, models, settings, context, changes, extensions, images, search |
| [`file.proto`](file.md) | `FileService` | The workspace's files, for the desktop app |
| [`config.proto`](config.md) | `ConfigService` | The settings files and API keys |
| [`worker.proto`](worker.md) | `WorkerService` | Workers and their runs |
