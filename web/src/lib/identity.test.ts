import { describe, expect, test } from "vitest";

import { clearIdentity, loadIdentity, saveIdentity } from "./identity";

/** A Storage backed by a Map. */
function memoryStorage(): Storage {
  const m = new Map<string, string>();
  return {
    get length() { return m.size; },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, String(v)),
  };
}

describe("identity", () => {
  test("is saved and loaded per tab", () => {
    const storage = memoryStorage();
    expect(loadIdentity(storage)).toBeNull();
    saveIdentity({ id: "u1", displayName: "Ana" }, storage);
    expect(loadIdentity(storage)).toEqual({ id: "u1", displayName: "Ana" });
    clearIdentity(storage);
    expect(loadIdentity(storage)).toBeNull();
  });

  test("anything malformed in storage counts as no identity", () => {
    const storage = memoryStorage();
    for (const bad of ["not json", "{}", '{"id":"u1"}', '{"id":1,"displayName":"Ana"}', "null"]) {
      storage.setItem("collab-tasks:identity", bad);
      expect(loadIdentity(storage), bad).toBeNull();
    }
  });

  test("works without storage at all, as on the server", () => {
    expect(loadIdentity(undefined)).toBeNull();
    expect(() => saveIdentity({ id: "u1", displayName: "Ana" }, undefined)).not.toThrow();
  });
});
