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

import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { PictureModelSchema, PictureTurnSchema } from "../gen/blitz/v1/workspace_pb";
import { againRequest, controls, defaultSettings, newSeed, standsAlone, extensionOf, fitSettings, loadSettings, keepSettings, ratioShape, refProblem, type Ref } from "./pictures";

const nano = create(PictureModelSchema, {
  ref: "gemini/gemini-nano-banana-2.1", name: "Nano", kind: "gemini", maxReferences: 14, maxCharacters: 4, maxObjects: 10, maxPictures: 1,
  sizes: ["1K", "2K", "4K"], ratios: ["1:1", "16:9", "4:1"], thinking: ["MINIMAL", "MEDIUM", "HIGH"], search: "image", formats: ["image/png", "image/jpeg"],
});
const imagen = create(PictureModelSchema, {
  ref: "gemini/imagen-4.0-generate-preview", name: "Imagen 4", kind: "imagen", maxPictures: 4, sizes: ["1K", "2K"], ratios: ["1:1", "16:9"],
  negativePrompt: true, people: true, seed: true, enhance: true, formats: ["image/png", "image/jpeg"],
});
const lite = create(PictureModelSchema, { ref: "gemini/lite", name: "Lite", kind: "gemini", maxReferences: 1, maxPictures: 1, ratios: ["1:1"], formats: ["image/png"] });

// The visibility matrix (spec_058 §4 of the owner's design): each model's controls from its catalog entry.
describe("a model's controls", () => {
  it("shows Gemini's references, locks its count, offers thinking and search", () => {
    expect(controls(nano)).toEqual({ references: true, countLocked: true, sizes: true, thinking: true, search: true, searchImages: true, avoid: false, people: false, seed: false, enhance: false, formats: true });
  });
  it("shows Imagen's count and advanced settings, hides references", () => {
    expect(controls(imagen)).toMatchObject({ references: false, countLocked: false, thinking: false, search: false, avoid: true, people: true, seed: true, enhance: true });
  });
  it("hides what a one-size, one-format model can't change", () => {
    expect(controls(lite)).toMatchObject({ sizes: false, formats: false, thinking: false });
    expect(controls(undefined).references).toBe(false);
  });
});

describe("changing the model", () => {
  it("keeps what the new model allows and says what it put back", () => {
    const s = { ...defaultSettings, model: nano.ref, ratio: "4:1", size: "4K", thinking: "HIGH", search: true, searchImages: true, format: "image/jpeg" };
    const { settings, resets } = fitSettings(s, imagen);
    expect(settings).toMatchObject({ model: imagen.ref, ratio: "1:1", size: "1K", thinking: "", search: false, searchImages: false, format: "image/jpeg" });
    expect(resets.map((r) => r.setting)).toEqual(["ratio", "size", "search"]);
    const back = fitSettings({ ...settings, count: 3, avoid: "blur", people: "ALLOW_ALL", seed: "7", enhance: true }, nano);
    expect(back.settings).toMatchObject({ count: 1, avoid: "", people: "", seed: "", enhance: false, thinking: "MEDIUM" });
    expect(back.resets.map((r) => r.setting)).toEqual(["count", "avoid", "people", "seed", "enhance"]);
    expect(fitSettings({ ...defaultSettings, format: "image/webp" }, lite).settings).toMatchObject({ size: "", format: "image/png" });
  });
});

describe("references", () => {
  const ref = (role: Ref["role"]): Ref => ({ key: role, role, name: "a.png", mime: "image/png", url: "" });
  it("refuses past the model's limits, in all and by role", () => {
    expect(refProblem(imagen, [], "object")).toBe("none");
    expect(refProblem(lite, [ref("style")], "style")).toBe("full");
    expect(refProblem(nano, [ref("character"), ref("character"), ref("character"), ref("character")], "character")).toBe("role");
    expect(refProblem(nano, [ref("character")], "style")).toBe("");
  });
});

describe("the rest", () => {
  it("draws each ratio's shape", () => {
    expect(ratioShape("1:1")).toEqual([18, 18]);
    expect(ratioShape("16:9")).toEqual([18, 10]);
    expect(ratioShape("1:8")).toEqual([3, 18]);
    expect(ratioShape("x")).toEqual([18, 18]);
  });
  it("names extensions", () => {
    expect(extensionOf("image/jpeg")).toBe("jpg");
    expect(extensionOf("image/webp")).toBe("webp");
    expect(extensionOf("image/png")).toBe("png");
  });
  it("keeps the settings per workspace, and opens on the defaults without storage", () => {
    expect(loadSettings("/w")).toEqual(defaultSettings);
    const store = new Map<string, string>();
    (globalThis as { localStorage?: unknown }).localStorage = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) };
    keepSettings("/w", { ...defaultSettings, ratio: "16:9" });
    expect(loadSettings("/w").ratio).toBe("16:9");
    delete (globalThis as { localStorage?: unknown }).localStorage;
  });
});

// Try again and Vary (PIC-42): a turn that stands alone, made again.
describe("making a turn again", () => {
  const turn = create(PictureTurnSchema, {
    model: "gemini/imagen-4.0-generate-preview", kind: "imagen", prompt: "a lighthouse", count: 2, aspectRatio: "16:9", size: "2K",
    avoid: "fog", people: "dont_allow", format: "image/jpeg", seed: 42, hasSeed: true, search: "",
    references: [{ file: "r-1.png", mime: "image/png", name: "a.png", path: "a.png", role: "style" }],
  });
  it("tries again as the turn was, its seed included", () => {
    const r = againRequest("/w", "pic-1", turn, { enhance: true, quality: 80 });
    expect(r).toMatchObject({ workspace: "/w", session: "pic-1", model: turn.model, prompt: "a lighthouse", count: 2, aspectRatio: "16:9", size: "2K", avoid: "fog", people: "dont_allow", format: "image/jpeg", seed: 42, useSeed: true, enhance: true, quality: 80, startFrom: "", estimate: false, search: false, searchImages: false });
    expect(r.references).toEqual([{ picture: "r-1.png", mime: "image/png", name: "a.png", role: "style" }]);
  });
  it("varies with a new seed", () => {
    expect(againRequest("/w", "pic-1", turn, { enhance: false, quality: 90 }, 7)).toMatchObject({ seed: 7, useSeed: true });
  });
  it("sends no seed when the turn had none, and no quality for a PNG", () => {
    const plain = create(PictureTurnSchema, { model: "openai/gpt-image-2", kind: "openai", prompt: "p", format: "image/png", search: "image" });
    expect(againRequest("/w", "s", plain, { enhance: false, quality: 90 })).toMatchObject({ useSeed: false, quality: 0, count: 1, search: true, searchImages: true });
  });
  it("offers it only where turns stand alone, with seeds in range", () => {
    expect(standsAlone("imagen")).toBe(true);
    expect(standsAlone("openai")).toBe(true);
    expect(standsAlone("gemini")).toBe(false);
    for (let i = 0; i < 20; i++) {
      const s = newSeed();
      expect(s).toBeGreaterThan(0);
      expect(s).toBeLessThan(2 ** 31);
    }
  });
});
