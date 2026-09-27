import { describe, expect, it } from "vitest";
import { staleReason } from "./serviceVersion";

describe("staleReason", () => {
  const info = { version: "1.4.0", executable: "/usr/lib/blitz-desktop/blitzd" };
  it("accepts the service the app expects", () => {
    expect(staleReason("1.4.0", info, true)).toBe("");
    expect(staleReason("dev", { ...info, version: "dev" }, true)).toBe("");
  });
  it("flags a service too old to describe itself", () => {
    expect(staleReason("1.4.0", undefined, true)).toBe("old");
  });
  it("flags a service whose program is gone, before its version", () => {
    expect(staleReason("1.4.0", info, false)).toBe("missing");
    expect(staleReason("1.5.0", info, false)).toBe("missing");
  });
  it("flags another version", () => {
    expect(staleReason("1.5.0", info, true)).toBe("mismatch");
    expect(staleReason("dev", info, true)).toBe("mismatch");
  });
});
