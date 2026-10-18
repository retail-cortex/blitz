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

// Studio's grade badges (spec_health_044 HL-15): each list's items show
// the judge's grade, from the dashboard Studio follows.

import { createContext, useContext } from "react";
import { t } from "../i18n";
import { gradeTone } from "../ui/charts";

/** What the badges show, by item key (kind/name): its grade, and whether a check is judging it now. */
export interface Grades {
  grades: Map<string, string>;
  judging?: string;
}

/** The grades Studio's lists show; none outside Studio. */
export const GradesContext = createContext<Grades | null>(null);

/** An item's grade as a small badge: a dot while it's judged, nothing when not judged. */
export function GradeBadge({ kind, name }: { kind: string; name: string }) {
  const g = useContext(GradesContext);
  if (!g) return null;
  const key = `${kind}/${name}`;
  if (g.judging === key) return <span className="grade-badge judging" title={t("desktop.health.judging_now")} aria-label={t("desktop.health.judging_now")} />;
  const grade = g.grades.get(key);
  if (!grade) return null;
  return (
    <span className={`grade-badge ${gradeTone(grade)}`} title={t("desktop.health.grade_is", { grade })}>
      {grade}
    </span>
  );
}
