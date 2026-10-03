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
import { acceptAttribute, acceptedAs, attachableFiles, clock, describeImage, isDocument, maxUploadBytes, readyIds, rejectReason, uploading, type Accepted, type Attachment } from "./attachments";

// What the service reports for a Claude model, and for Gemini.
const claude: Accepted[] = [
  { kind: "image", mimeTypes: ["image/png", "image/jpeg"], extensions: [".png", ".jpg", ".jpeg"], maxBytes: 20 << 20 },
  { kind: "document", mimeTypes: ["application/pdf"], extensions: [".pdf"], maxBytes: 32 << 20 },
  { kind: "text", mimeTypes: ["text/plain"], extensions: [".txt", ".md", ".csv"], maxBytes: 1 << 20 },
];
const gemini: Accepted[] = [...claude, { kind: "video", mimeTypes: ["video/mp4", "video/quicktime"], extensions: [".mp4", ".mov"], maxBytes: 2 ** 31 }];

describe("attachments", () => {
  it("accepts what the model takes, by name or type", () => {
    expect(acceptedAs(claude, { type: "image/png", name: "a.png" })?.kind).toBe("image");
    expect(acceptedAs(claude, { type: "", name: "notes.MD" })?.kind).toBe("text");
    expect(acceptedAs(claude, { type: "image/jpeg", name: "pasted" })?.kind).toBe("image");
    expect(acceptedAs(claude, { type: "video/mp4", name: "clip.mp4" })).toBeUndefined();
    expect(acceptedAs(gemini, { type: "video/mp4", name: "clip.mp4" })?.kind).toBe("video");
  });
  it("says why a file can't be attached", () => {
    expect(rejectReason({ type: "image/png", size: 10, name: "a.png" }, claude, "claude-sonnet-5")).toBe("");
    expect(rejectReason({ type: "video/mp4", size: 10, name: "clip.mp4" }, claude, "claude-sonnet-5")).toBe("claude-sonnet-5 can't take clip.mp4");
    expect(rejectReason({ type: "image/png", size: (20 << 20) + 1, name: "big.png" }, claude, "m")).toMatch("larger than 20 MB");
    expect(rejectReason({ type: "video/mp4", size: maxUploadBytes + 1, name: "talk.mp4" }, gemini, "m")).toMatch("put it in the workspace");
    expect(rejectReason({ type: "text/plain", size: (1 << 20) + 1, name: "log.txt" }, claude, "m")).toMatch("larger than 1 MB");
  });
  it("builds the picker's accept list and filters files", () => {
    expect(acceptAttribute(claude)).toBe(".png,.jpg,.jpeg,image/png,image/jpeg,.pdf,application/pdf,.txt,.md,.csv,text/plain");
    const f = (name: string, type: string) => new File(["x"], name, { type });
    expect(attachableFiles([f("a.png", "image/png"), f("b.mp4", "video/mp4"), f("c.md", "")], claude).map((x) => x.name)).toEqual(["a.png", "c.md"]);
    expect(attachableFiles(null, claude)).toEqual([]);
    expect(isDocument({ type: "", name: "dropped.PDF" })).toBe(true);
  });
  it("sends only uploaded files and waits for the rest", () => {
    const list: Attachment[] = [
      { key: "1", name: "a", url: "u1", id: "x" },
      { key: "2", name: "b", url: "u2" },
      { key: "3", name: "c", url: "u3", error: "too big" },
    ];
    expect(readyIds(list)).toEqual(["x"]);
    expect(uploading(list)).toBe(true);
    expect(uploading([list[0], list[2]])).toBe(false);
  });
  it("describes an uploaded file", () => {
    expect(describeImage({ width: 1280, height: 720, size: 2n * 1024n * 1024n, resized: true })).toBe("1280×720 · 2.0 MB · scaled down");
    expect(describeImage({ width: 10, height: 10, size: 100, resized: false })).toBe("10×10 · 1 KB");
    expect(describeImage({ width: 0, height: 0, size: 3n << 20n, resized: false, mimeType: "application/pdf", pages: 12 })).toBe("12 pages · 3.0 MB");
    expect(describeImage({ width: 0, height: 0, size: 2048, resized: false, mimeType: "application/pdf", pages: 1 })).toBe("1 page · 2 KB");
    expect(describeImage({ width: 0, height: 0, size: 5 << 20, resized: false, kind: "audio", seconds: 192 })).toBe("3:12 · 5.0 MB");
    expect(describeImage({ width: 0, height: 0, size: 2048, resized: false, kind: "video" })).toBe("2 KB");
    expect(describeImage({ width: 0, height: 0, size: 2048, resized: false, kind: "text" })).toBe("2 KB");
  });
  it("formats a length", () => {
    expect([clock(5), clock(192), clock(3725)]).toEqual(["0:05", "3:12", "1:02:05"]);
  });
});
