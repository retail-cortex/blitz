import { afterEach, describe, expect, it } from "vitest";
import enUS from "../../../../pkg/i18n/locales/en-US.json";
import { chain, language, pseudo, resolveLanguage, setLanguage, t, tn } from "./i18n";

afterEach(() => setLanguage("en-US"));

describe("i18n", () => {
  it("looks from the most specific catalog to English", () => {
    expect(chain("fr-CA").map((c) => c.meta.locale)).toEqual(["fr-CA", "en-US"]);
    expect(chain("fr").map((c) => c.meta.locale)).toEqual(["fr-CA", "en-US"]);
    expect(chain("es-MX").map((c) => c.meta.locale)).toEqual(["es", "en-US"]);
    expect(chain("de").map((c) => c.meta.locale)).toEqual(["en-US"]);
  });
  it("follows the system unless a language is chosen", () => {
    expect(resolveLanguage("system", ["de-DE", "fr-FR", "en-US"])).toBe("fr-CA");
    expect(resolveLanguage("system", ["en-GB", "es"])).toBe("en-US");
    expect(resolveLanguage("system", [])).toBe("en-US");
    expect(resolveLanguage("es", ["fr-CA"])).toBe("es");
    expect(resolveLanguage("en-XA", [])).toBe("en-XA");
  });
  it("fills placeholders and plurals in the chosen language", () => {
    setLanguage("fr-CA");
    expect(language()).toBe("fr-CA");
    expect(t("desktop.approval.always", { scope: "shell(ls)" })).toBe("Toujours permettre shell(ls)");
    expect(tn("desktop.tools.used", 1)).toBe("1 outil utilisé");
    expect(tn("desktop.tools.used", 1500)).toMatch(/^1\s500 outils utilisés$/);
    setLanguage("es");
    expect(tn("desktop.tools.used", 2)).toBe("Usó 2 herramientas");
    expect(t("no.such.key")).toBe("no.such.key");
  });
  it("marks English text in the pseudo-locale, placeholders kept", () => {
    expect(pseudo("Remove {name}")).toBe("⟦Rémóvé {name}⟧");
    setLanguage("en-XA");
    expect(t("desktop.attach.remove", { name: "a.png" })).toBe("⟦Rémóvé a.png⟧");
    expect(language()).toBe("en-US");
  });
  it("has one and other forms for every plural the page uses", () => {
    const keys = Object.keys((enUS as { messages: Record<string, string> }).messages).filter((k) => k.startsWith("desktop."));
    for (const k of keys.filter((k) => k.endsWith(".one"))) expect(keys).toContain(k.replace(/\.one$/, ".other"));
  });
});
