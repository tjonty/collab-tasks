import { ApiStatus } from "@/components/api-status";

export default function Home() {
  return (
    <main className="mx-auto flex max-w-3xl flex-col gap-4 px-6 py-24 font-sans">
      <h1 className="text-3xl font-semibold tracking-tight">Collab Tasks</h1>
      <ApiStatus />
    </main>
  );
}
