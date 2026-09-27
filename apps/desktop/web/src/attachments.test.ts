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

import { describe, expect, it } from "vitest";
import { describeImage, maxImageBytes, readyIds, rejectReason, uploading, type Attachment } from "./attachments";

describe("attachments", () => {
  it("accepts images up to the limit", () => {
    expect(rejectReason({ type: "image/png", size: 10, name: "a.png" })).toBe("");
    expect(rejectReason({ type: "text/plain", size: 10, name: "a.txt" })).toMatch("isn't an image");
    expect(rejectReason({ type: "image/png", size: maxImageBytes + 1, name: "big.png" })).toMatch("larger than 20 MB");
  });
  it("sends only uploaded images and waits for the rest", () => {
    const list: Attachment[] = [
      { key: "1", name: "a", url: "u1", id: "x" },
      { key: "2", name: "b", url: "u2" },
      { key: "3", name: "c", url: "u3", error: "too big" },
    ];
    expect(readyIds(list)).toEqual(["x"]);
    expect(uploading(list)).toBe(true);
    expect(uploading([list[0], list[2]])).toBe(false);
  });
  it("describes an uploaded image", () => {
    expect(describeImage({ width: 1280, height: 720, size: 2n * 1024n * 1024n, resized: true })).toBe("1280×720 · 2.0 MB · scaled down");
    expect(describeImage({ width: 10, height: 10, size: 100, resized: false })).toBe("10×10 · 1 KB");
  });
});
