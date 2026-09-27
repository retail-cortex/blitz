# spec_i18n_004 — Internationalisation

| | |
|---|---|
| Status | Implemented. Reverse-engineered from `53f8c53` (2026-09-24) and kept with the code since; its paths and names checked 2026-09-27 (`//tools/specs`). |
| Source | `pkg/i18n/i18n.go`, `pkg/i18n/locales/{en-US,es,fr-CA}.json`; `SetupLocale`/`SetLocale` in `pkg/engine`; `docs/TRANSLATING.md` |
| Tests | `pkg/i18n/i18n_test.go`, `apps/cli/internal/tui/i18n_lint_test.go` (`TestNoUntranslatedOutput`), `apps/cli/internal/tui/locale_test.go`, `apps/cli/locale_test.go` |
| Depends on | [spec_config_002](spec_config_002.md) |

## 1. Purpose

The interactive interface speaks English (`en-US`, source), Spanish (`es`) and Canadian French (`fr-CA`). Any other language changes the language the model replies in while menus stay English until a catalog exists. Catalogs can be added or corrected without rebuilding.

## 2. Catalogs

- **I18N-01** One JSON file per language: `{"meta":{"locale","name","english_name"},"messages":{key: text}}`. Shipped catalogs are embedded; extra catalogs load from `ui.locales_dir` (`~/.blitz/locales`). Later files add keys and override earlier translations (a file with just one key corrects one phrase). Unparseable external files are warnings and the rest still load. Empty `name`/`english_name` default to CLDR names.
- **I18N-02** Every key must exist in `en-US`. Placeholders are `{name}` (order may change; names must match). Plurals use `.one`/`.other` keys selected by CLDR rules for common cases (French treats 0 as singular; ja, zh, ko, th, vi, id always use `.other`).
- **I18N-03** Answer letters in brackets (`[y] [s] [a] [n] [d] [k] [w] [c]`) are what the user types and must be kept in translations.
- **I18N-04** Lookup falls back along the locale chain (`fr-CA` → `fr` → `en-US`); a bare language borrows the lowest-tag regional catalog (`fr` → `fr-CA`).
- **I18N-05** `Problems(tag)` reports unknown keys and placeholder mismatches (plural forms compared against English `.other`); `Missing(tag)` lists untranslated keys. Tests enforce: no missing or unknown keys, matching placeholders, answer letters intact, for every shipped catalog.
- **I18N-06** Pseudo-locale `en-XA` renders English accented and bracketed (`⟦Éxít⟧`), placeholders untouched, to find text never moved into a catalog; `TestNoUntranslatedOutput` enforces this in CI.
- **I18N-07** Convention: every new user-facing string goes into all three shipped catalogs.

## 3. Resolving input

- **I18N-10** `Resolve(input)` accepts BCP 47 tags in any case with `-` or `_` (`es`, `es-ES`, `ES_es`), keeps the language when the region is invalid (`ES-sp` → `es`), and matches catalog names in English or natively (`spanish`, `español`). Otherwise `ErrUnknownLocale`.
- **I18N-11** Invalid `ui.locale` at startup warns and uses `en-US`.

## 4. Two languages: interface and reply

- **I18N-20** The **interface** language is per process (package-level current localizer, `T`/`N`). The **reply** language is per workspace: for non-English locales (not the pseudo-locale) the agents' instructions gain "## Response Language … Write your replies to the user in <Language> … Keep code, identifiers, file paths, commands, and tool output exactly as they are."
- **I18N-21** `/locale` shows the current language and available catalogs; `/locale <code>` switches both (the client switches its own interface), rebuilds instructions, and saves `[ui] locale` in place ([spec_config_002](spec_config_002.md) CFG-23).

## 5. Not translated (policy)
- **I18N-30** `doctor` output, `--help` and CLI errors (so they can be pasted into bug reports); text the model reads (tool descriptions, tool results); error details from lower layers; code, paths, commands and tool output.
