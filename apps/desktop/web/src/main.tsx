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

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { EditorPanel } from "./EditorPanel";
import { editorConfig } from "./host";
import "./m3.css";
import "./app.css";

async function start() {
  // A development build can talk to a fake service (?fake), for working on
  // the page without one. Production builds leave this out.
  if (import.meta.env.DEV && new URLSearchParams(location.search).has("fake")) {
    const { installFake } = await import("./dev/fake");
    installFake();
  }
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      {editorConfig() ? <EditorPanel /> : <App />}
    </StrictMode>,
  );
}
start();
