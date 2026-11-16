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
import { workers } from "./api";
import { message } from "./errors";

/**
 * The home workspace (~/.blitz/home, spec_023_workers WK-60), where the
 * global jobs and the global evals run: its folder once the service has
 * said (made when missing), or why it couldn't.
 */
export function useHomeWorkspace(): { dir: string; error: string } {
  const [home, setHome] = useState({ dir: "", error: "" });
  useEffect(() => {
    let live = true;
    workers.listAllWorkers({}).then(
      (r) => live && setHome({ dir: r.home, error: "" }),
      (e) => live && setHome({ dir: "", error: message(e) }),
    );
    return () => {
      live = false;
    };
  }, []);
  return home;
}
