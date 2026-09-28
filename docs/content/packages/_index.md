---
title: Shared packages
weight: 45
---

The packages under `pkg/` are shared by the apps. Each is a Go package in the one module, `github.com/retail-cortex/blitz`, and every package and exported name is documented in the code (`go doc`, or your editor). These pages say what each is for and who uses it. [Architecture](../architecture/_index.md#the-dependency-rules) has the rules for who may depend on what.

| Package | Layer | |
|---|---|---|
| [`api`](api.md) | Contract | `Backend`, and the values, events and errors front ends see |
| [`client`](client.md) | Contract | `Backend` over the service's socket |
| [`socket`](socket.md) | Contract | Where the service listens, and how to reach it |
| [`engine`](engine.md) | Engine | Blitz without a user interface, and its subpackages |
| [`config`](config.md) | Shared | Settings: loading, layering and editing `.env.toml` |
| [`secrets`](secrets.md) | Shared | API keys in the OS keychain |
| [`i18n`](i18n.md) | Shared | Message catalogs and locales |
| [`images`](images.md) | Shared | Preparing pictures for vision models |
| [`observability`](observability.md) | Shared | The diagnostic log and OpenTelemetry |
| [`redact`](redact.md) | Shared | Masking secrets before they're written |
| [`legal`](legal.md) | Shared | The license texts every program shows |
| [`loginitem`](loginitem.md) | Shared | Starting the service at login |
| [`textutil`](textutil.md) | Shared | Small string helpers |

The API protos, `proto/blitz/v1`, are shared too: see [the service API](../architecture/service-api.md).
