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

// Small charts drawn in SVG with the theme's colours (spec_health_044
// HL-03): a score ring, bars, stacked day columns with a line, and a
// sparkline. Each has a text alternative.

import { chartGeometry, ringDash } from "./chartMath";

/** A grade's colour class: A–D, or none. */
export const gradeTone = (grade: string) => (grade ? `grade-${grade.toLowerCase()}` : "grade-none");

/** A score (1–5) as a ring with its grade inside; an empty ring when not judged. */
export function ScoreRing({ score, grade, size = 64, label, sub }: { score: number; grade: string; size?: number; label: string; sub?: string }) {
  const r = size / 2 - 5;
  const { dash, gap } = ringDash(score, r);
  return (
    <svg className={`score-ring ${gradeTone(grade)}`} width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img" aria-label={label}>
      <circle className="score-ring-track" cx={size / 2} cy={size / 2} r={r} />
      {score > 0 && <circle className="score-ring-value" cx={size / 2} cy={size / 2} r={r} strokeDasharray={`${dash} ${gap}`} transform={`rotate(-90 ${size / 2} ${size / 2})`} />}
      <text className="score-ring-grade" x="50%" y={sub ? "46%" : "50%"} dominantBaseline="central" textAnchor="middle" style={{ fontSize: size * 0.32 }}>
        {grade || "–"}
      </text>
      {sub && (
        <text className="score-ring-sub" x="50%" y="70%" dominantBaseline="central" textAnchor="middle" style={{ fontSize: size * 0.14 }}>
          {sub}
        </text>
      )}
    </svg>
  );
}

/** One bar of Bars: its label, value (0–1) and the text shown beside it. */
export interface Bar {
  label: string;
  value: number;
  text: string;
  tone?: string;
}

/** Horizontal bars, 0 to 1. */
export function Bars({ bars, label }: { bars: Bar[]; label: string }) {
  return (
    <div className="chart-bars" role="list" aria-label={label}>
      {bars.map((b) => (
        <div key={b.label} className="chart-bar" role="listitem" aria-label={`${b.label}: ${b.text}`}>
          <span className="chart-bar-label ellipsis">{b.label}</span>
          <span className="chart-bar-track">
            <span className={`chart-bar-fill ${b.tone ?? ""}`} style={{ width: `${Math.max(0, Math.min(1, b.value)) * 100}%` }} />
          </span>
          <span className="chart-bar-text">{b.text}</span>
        </div>
      ))}
    </div>
  );
}

/** One column of Columns: its parts, stacked, and a line value. */
export interface Column {
  key: string;
  parts: { value: number; tone: string }[];
  line: number;
  title: string;
}

/** Stacked columns (one per day) with a line over them on its own scale. */
export function Columns({ columns, label, height = 120 }: { columns: Column[]; label: string; height?: number }) {
  const width = 600;
  const g = chartGeometry(
    columns.map((c) => c.parts.reduce((s, p) => s + p.value, 0)),
    columns.map((c) => c.line),
    width,
    height,
  );
  return (
    <svg className="chart-columns" viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label={label}>
      <line className="chart-axis" x1="0" y1={height - 0.5} x2={width} y2={height - 0.5} />
      {columns.map((c, i) => {
        let y = height;
        return (
          <g key={c.key}>
            <title>{c.title}</title>
            {c.parts.map((p, j) => {
              const h = p.value * g.columnScale;
              y -= h;
              return h > 0 ? <rect key={j} className={`chart-part ${p.tone}`} x={i * g.step + g.step * 0.15} y={y} width={g.step * 0.7} height={h} rx="1.5" /> : null;
            })}
          </g>
        );
      })}
      {g.line && <polyline className="chart-line" points={g.line} vectorEffect="non-scaling-stroke" />}
    </svg>
  );
}

/** A sparkline of values, oldest first; nothing for fewer than two. */
export function Sparkline({ values, label, width = 80, height = 20 }: { values: number[]; label: string; width?: number; height?: number }) {
  if (values.length < 2) return null;
  const g = chartGeometry([], values, width, height - 2);
  return (
    <svg className="sparkline" width={width} height={height} viewBox={`0 -1 ${width} ${height}`} role="img" aria-label={label}>
      <polyline points={g.line} vectorEffect="non-scaling-stroke" />
    </svg>
  );
}
