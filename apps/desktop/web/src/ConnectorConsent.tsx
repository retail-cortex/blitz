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

import { useCallback, useEffect, useState } from "react";
import { mdiAccountKeyOutline, mdiCheckCircleOutline, mdiRefresh } from "@mdi/js";
import { Button, Icon, useSnackbar } from "@blitz/webui";
import { config } from "./api";
import { openURL } from "./desktop";
import { message } from "./errors";
import type { GetConnectorConsentResponse } from "./gen/blitz/v1/config_pb";
import { t } from "./i18n";

/**
 * Data as you (spec_053 EN-76): the organization's connectors that query
 * as the person, with their own Google account; Allow opens Google's
 * consent in the browser, Check again reads whether it's done, Revoke
 * forgets it. Nothing when none queries as them.
 */
export function ConnectorConsentCard() {
  const snack = useSnackbar();
  const [state, setState] = useState<GetConnectorConsentResponse | null>(null);
  const load = useCallback(() => {
    config.getConnectorConsent({}).then(setState, () => setState(null));
  }, []);
  useEffect(load, [load]);
  if (!state || state.connectors.length === 0 || (!state.consented && !state.url)) return null;
  const revoke = async () => {
    try {
      await config.revokeConnectorConsent({});
      snack(t("desktop.consent.revoked"));
      load();
    } catch (e) {
      snack(message(e), { error: true });
    }
  };
  return (
    <section className="card stack" style={{ gap: 8 }}>
      <span className="t-title-sm">{t("desktop.consent.title")}</span>
      <span className="t-body-sm muted">{t("desktop.consent.about", { connectors: state.connectors.join(", ") })}</span>
      <div className="row" style={{ gap: 8 }}>
        {state.consented ? (
          <>
            <span className="row t-body-sm spacer" style={{ gap: 6 }}>
              <Icon path={mdiCheckCircleOutline} size="sm" />
              {t("desktop.consent.allowed")}
            </span>
            <Button small onClick={() => void revoke()}>
              {t("desktop.consent.revoke")}
            </Button>
          </>
        ) : (
          <>
            <Button small variant="filled" icon={mdiAccountKeyOutline} onClick={() => void openURL(state.url)}>
              {t("desktop.consent.allow")}
            </Button>
            <Button small icon={mdiRefresh} onClick={load}>
              {t("desktop.consent.check")}
            </Button>
          </>
        )}
      </div>
    </section>
  );
}
