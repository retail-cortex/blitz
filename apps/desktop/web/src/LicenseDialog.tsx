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

import { useEffect, useState } from "react";
import { mdiScaleBalance } from "@mdi/js";
import { licenseText, type LicenseText } from "./desktop";
import { message } from "./errors";
import { t } from "./i18n";
import { Button, Dialog, Segmented } from "./ui/controls";

/**
 * Blitz's license terms (spec_release_readiness_030 RR-05): the NOTICE,
 * the Apache License and the third-party notices, as the app embeds them.
 * Opened from Settings › About and by /license.
 */
export function LicenseDialog({ initial, onClose }: { initial: LicenseText; onClose: () => void }) {
  const [which, setWhich] = useState<LicenseText>(initial);
  const [text, setText] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let live = true;
    setText("");
    setError("");
    licenseText(which).then(
      (s) => live && setText(s),
      (e) => live && setError(message(e)),
    );
    return () => {
      live = false;
    };
  }, [which]);
  return (
    <Dialog title={t("desktop.license.title")} icon={mdiScaleBalance} onClose={onClose} wide footer={<Button onClick={onClose}>{t("desktop.done")}</Button>}>
      <div className="stack" style={{ gap: 12 }}>
        <Segmented<LicenseText>
          label={t("desktop.license.title")}
          value={which}
          onChange={setWhich}
          options={[
            { value: "notice", label: t("desktop.license.notice") },
            { value: "full", label: t("desktop.license.full") },
            { value: "third-party", label: t("desktop.license.third_party") },
          ]}
        />
        {error ? <p className="error-text">{error}</p> : <pre className="license-text">{text || t("desktop.checking")}</pre>}
      </div>
    </Dialog>
  );
}
