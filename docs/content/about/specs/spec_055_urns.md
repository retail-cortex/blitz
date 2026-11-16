---
title: "055 · Resource names"
weight: 55
---

*Everything Blitz and a Blitz Enterprise service keep has one name, a URN such as `urn:blitz:runs:<run-id>`: unique on its own, the same whichever store holds it, and something a client can ask the service about* (`spec_055_urns`)

| | |
|---|---|
| Status | **Built** (2026-11-02: `pkg/urn`; Blitz lists and saves a run's files from an organization's service in Studio › Jobs. Drafted 2026-11-01, the owner: "by adopting and considering everything as a URN, it becomes both discoverable and unique", after asking how two people's workspaces named `home` would collide) |
| Source | `pkg/urn` (`Name`, `Parse`, `New`, the kinds) |
| Tests | `pkg/urn/urn_test.go` |
| Depends on | Blitz Enterprise Server (the organization's service, its runs, artifacts and workspaces) |

## 1. Purpose

A workspace's name is a label: two people can each call theirs `home`. Its handle, the short name in its address, is unique only where it's kept. Paths in a bucket, rows in Postgres and folders on a disk each name the same thing differently, and moving a thing from one store to another renames it.

A resource name fixes that. Each thing has one name made only of IDs, which never change and never collide. Where the thing is kept (a bucket, a table, a folder) comes from its name, not the other way round. A client that holds a name can ask the service what it is and what's under it.

## 2. The form

- **RN-01** A name is a URN (RFC 8141) in the namespace `blitz`:

  ```
  urn:blitz:<kind>:<id>[:<id>…][/<path>]
  ```

  - `kind` is one of the kinds in RN-10, in lower case.
  - Each `id` is the ID the service gave the thing: a UUID in its canonical lower-case form. A name never holds a display name, handle, email address or anything else that can change or be reused.
  - The `path`, for kinds that hold files (RN-12), names a file under the thing: `/`-separated, each segment percent-encoded as RFC 3986 says for a path segment, never empty, `.` or `..`.
- **RN-02** Names compare as strings after `urn:` and `blitz:` are lower-cased (RFC 8141 §3.1). `Parse` returns them in that form; `String` writes only that form.
- **RN-03** `urn:blitz:<kind>` alone names every resource of that kind the caller may see: the collection. It's what a client lists.
- **RN-04** A name says nothing about access. Holding one doesn't let anyone read the thing: the service checks the caller's roles for each request, as it does for any other.
- **RN-05** `blitz` isn't registered with IANA. Names never leave Blitz and its services, so nothing outside needs to resolve them; registering it later doesn't change a name.

## 3. The kinds

- **RN-10** Things with their own ID take one `id`:

  | Kind | Names | Example |
  |---|---|---|
  | `users` | a person in the organization | `urn:blitz:users:7c1e…` |
  | `groups` | a group of people | `urn:blitz:groups:0d4a…` |
  | `workspaces` | a workspace | `urn:blitz:workspaces:9b2f…` |
  | `jobs` | a job on the service | `urn:blitz:jobs:51aa…` |
  | `runs` | one run of a job | `urn:blitz:runs:e803…` |
  | `content` | content someone shared: a chat's copy, a rendered page | `urn:blitz:content:4f19…` |

  Shared content isn't `artifacts`: that kind is a run's (RN-12), and the two are kept, kept for and removed differently.

- **RN-11** A thing's ID is unique across the organization on its own, so a name never needs its parent to be unique. A run's name is `urn:blitz:runs:<run-id>`, not `urn:blitz:jobs:<job-id>:<run-id>`.
- **RN-12** `artifacts` names what a run left behind, and takes the IDs of the workspace, the job and the run, in that order, so a store can find, keep or remove one workspace's or one job's together:

  ```
  urn:blitz:artifacts:<workspace-id>:<job-id>:<run-id>
  urn:blitz:artifacts:<workspace-id>:<job-id>:<run-id>/report.csv
  urn:blitz:artifacts:<workspace-id>:<job-id>:<run-id>/out/charts/sales%20by%20week.png
  ```

  Without a path it's all of the run's artifacts; with one, one file.
- **RN-13** A new kind is added here first. An unknown kind is refused by `Parse`, so a typo is an error, not a name nobody can find.

## 4. On this computer

- **RN-40** Blitz itself reads and writes names only for things it gets from an organization's service: a run's artifacts, a job, a workspace it syncs. It doesn't give names to things kept only on this computer; a local kind, if one is wanted, is added here first (RN-13).
