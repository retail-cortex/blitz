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


// The window's one drawer: which view holds it, and what happens to the
// one it replaces (closed, or asked about first when it has unsaved
// changes). Kept apart from React so the rules are tested plainly.

/** A drawer, as the host knows it. */
export interface Claim {
  id: number;
  /** Closes it: its owner forgets it. */
  close: () => void;
  /** Whether it holds unsaved changes. */
  dirty: () => boolean;
  /** What it's called, for the question. */
  name: () => string;
}

/** A question for the user: discard a drawer's unsaved changes, or keep them. */
export interface Ask {
  name: string;
  discard: () => void;
  keep: () => void;
}

/**
 * Who holds the drawer. show hears which drawer is shown (null: none);
 * ask puts a question to the user.
 */
export class DrawerClaims {
  private held: Claim | null = null;

  constructor(
    private readonly show: (id: number | null) => void,
    private readonly ask: (a: Ask) => void,
  ) {}

  /**
   * c takes the drawer. The drawer it replaces closes, or, with unsaved
   * changes, asks first: keeping them closes c instead and shows the old
   * one again. Returns what to call when c goes away.
   */
  claim(c: Claim): () => void {
    const prev = this.held;
    this.held = c;
    this.show(c.id);
    if (prev) {
      if (prev.dirty()) {
        this.ask({
          name: prev.name(),
          discard: () => prev.close(),
          keep: () => {
            this.held = prev;
            this.show(prev.id);
            c.close();
          },
        });
      } else prev.close();
    }
    return () => {
      if (this.held?.id === c.id) {
        this.held = null;
        this.show(null);
      }
    };
  }

  /** The user closes c (✕, Escape): asked first when it has unsaved changes. */
  requestClose(c: Claim): void {
    if (c.dirty()) this.ask({ name: c.name(), discard: c.close, keep: () => {} });
    else c.close();
  }
}
