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

// The guide's sections, each a link of its own in the Help menu
// (DSK-103), in the site's order. docs.test.ts checks each is a page the
// app carries.
import { mdiApps, mdiCompassOutline, mdiRocketLaunchOutline, mdiSchoolOutline, mdiTagOutline } from "@mdi/js";

/** A section of the guide: its page, its label's catalog key, its icon. */
export interface DocSection {
  page: string;
  labelKey: string;
  icon: string;
}

/** The sections, in the site's order. */
export const docSections: DocSection[] = [
  { page: "getting-started", labelKey: "desktop.menu.docs.getting_started", icon: mdiRocketLaunchOutline },
  { page: "tutorials", labelKey: "desktop.menu.docs.tutorials", icon: mdiSchoolOutline },
  { page: "products", labelKey: "desktop.menu.docs.products", icon: mdiApps },
  { page: "guide", labelKey: "desktop.menu.docs.guide", icon: mdiCompassOutline },
  { page: "releases", labelKey: "desktop.menu.docs.releases", icon: mdiTagOutline },
];
