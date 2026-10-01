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

// A fenced code block: highlighted, with its language and a copy button.
import { useState } from "react";
import { mdiCheck, mdiContentCopy } from "@mdi/js";
import { highlight, languageFor } from "./highlight";
import { t } from "./i18n";
import { IconButton } from "./ui/controls";

/** A code block in Markdown, highlighted for lang. */
export function CodeBlock({ children, lang }: { children: string; lang: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="code-block">
      <div className="code-head">
        <span className="t-label muted">{lang || "text"}</span>
        <IconButton
          icon={copied ? mdiCheck : mdiContentCopy}
          label={copied ? t("desktop.code.copied") : t("desktop.code.copy")}
          small
          onClick={() =>
            navigator.clipboard?.writeText(children).then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            })
          }
        />
      </div>
      <pre>
        <code className="hljs">{highlight(children, languageFor(lang))}</code>
      </pre>
    </div>
  );
}
