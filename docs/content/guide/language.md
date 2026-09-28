---
title: Language
weight: 90
---

Spec: [internationalisation](../about/specs/spec_i18n_004.md).

The interface speaks English (`en-US`, the default), Spanish (`es`) and Canadian French (`fr-CA`). `/locale` shows the current language; `/locale es` switches and saves `[ui] locale = "es"`. Codes are forgiving: `es-ES`, `es_MX`, `spanish` and `español` all work. The desktop app follows the system's language by default and switches in **Settings › Appearance**, from the same catalogs.

Any other language (`/locale ja`) changes the language the model replies in, while the interface stays in English until someone adds a catalog. Code, paths, commands and tool output are never translated, and `blitz doctor`, `--help` and command-line errors stay in English so they can be shared in bug reports.

To add or correct a language, put a JSON catalog in `~/.blitz/locales/`; see [translating](../development/translating.md).
