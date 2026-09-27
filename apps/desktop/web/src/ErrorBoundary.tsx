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

import { Component, type ReactNode } from "react";
import { t } from "./i18n";

/**
 * Catches a crash while drawing the page, so the window shows what went
 * wrong and a way on (reload) instead of freezing or going blank.
 */
export class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  render() {
    const e = this.state.error;
    if (!e) return this.props.children;
    const details = `${e.name}: ${e.message}\n${e.stack ?? ""}`;
    return (
      <div className="splash">
        <div className="drag-region" />
        <div className="splash-card">
          <h1 className="t-headline">{t("desktop.crash.title")}</h1>
          <p className="muted">{t("desktop.crash.body")}</p>
          <pre className="json crash-details">{details}</pre>
          <div className="row">
            <button className="btn filled" onClick={() => location.reload()}>
              {t("desktop.crash.reload")}
            </button>
            <button className="btn text" onClick={() => navigator.clipboard?.writeText(details)}>
              {t("desktop.crash.copy")}
            </button>
          </div>
        </div>
      </div>
    );
  }
}
