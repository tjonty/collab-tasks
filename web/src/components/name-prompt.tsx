"use client";

import { useState } from "react";

/** Asks for a display name and creates the user. */
export function NamePrompt({ onCreate }: { onCreate: (displayName: string) => Promise<void> }) {
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const trimmed = name.trim();

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!trimmed) return;
    setBusy(true);
    setError(null);
    try {
      await onCreate(trimmed);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit} className="mx-auto mt-32 flex w-full max-w-sm flex-col gap-3 px-6">
      <h1 className="text-2xl font-semibold tracking-tight">Collab Tasks</h1>
      <p className="text-sm text-zinc-600 dark:text-zinc-400">
        Pick a display name. Each tab can be someone different.
      </p>
      <label className="flex flex-col gap-1 text-sm">
        Display name
        <input
          autoFocus
          maxLength={40}
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="rounded border border-zinc-300 bg-transparent px-3 py-2 dark:border-zinc-700"
        />
      </label>
      <button
        type="submit"
        disabled={!trimmed || busy}
        className="rounded bg-zinc-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-40 dark:bg-zinc-100 dark:text-zinc-900"
      >
        Continue
      </button>
      {error && <p className="text-sm text-red-600">{error}</p>}
    </form>
  );
}
