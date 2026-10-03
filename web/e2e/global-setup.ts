// Fail fast, and say why, when the stack isn't running.
export default async function globalSetup() {
  const web = process.env.E2E_BASE_URL ?? "http://localhost:3000";
  const api = `${process.env.E2E_API_URL ?? "http://localhost:8080"}/healthz`;
  for (const [name, url] of [["web app", web], ["API", api]]) {
    const ok = await fetch(url).then((r) => r.ok, () => false);
    if (!ok) {
      throw new Error(`The ${name} isn't answering at ${url}. Start the stack first: docker compose up --build`);
    }
  }
}
