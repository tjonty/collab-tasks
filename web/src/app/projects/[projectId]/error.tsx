"use client"; // Error boundaries must be Client Components

import Link from "next/link";

export default function ProjectError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return (
    <main className="mx-auto mt-24 flex max-w-md flex-col gap-3 px-6">
      <h2 className="text-lg font-semibold">This project couldn&apos;t be shown</h2>
      <p className="text-sm text-muted">{error.message}</p>
      <div className="flex gap-3 text-sm">
        <button type="button" onClick={() => retry()} className="rounded border border-line-strong bg-surface px-3 py-1 hover:bg-subtle">
          Try again
        </button>
        <Link href="/" className="px-3 py-1 underline">
          All projects
        </Link>
      </div>
    </main>
  );
}
