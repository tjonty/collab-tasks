// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, test } from "vitest";

import { createApi, type ApiClient } from "@/lib/api/client";
import type { User } from "@/lib/api/types";

import { useUsers } from "./users";

afterEach(cleanup);

const ana: User = { id: "01a0ff4b-6485-7357-ac78-9da028c90001", displayName: "Ana" };
const raj: User = { id: "01a0ff4b-6485-7357-ac78-9da028c90002", displayName: "Raj" };

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

/** Shows each id's name, as task rows and comments do. */
function Names({ api, ids }: { api: ApiClient; ids: string[] }) {
  const { nameOf } = useUsers(api, ids);
  return (
    <ul>
      {ids.map((id) => (
        <li key={id}>{nameOf(id)}</li>
      ))}
    </ul>
  );
}

/** An API whose GET /users answers with the next scripted response. */
function setup(...answers: Response[]) {
  const requests: Request[] = [];
  const api = createApi({
    baseUrl: "http://api.test",
    userId: () => ana.id,
    onUnknownUser: () => {},
    fetch: async (req) => {
      requests.push(req);
      return answers.shift() ?? json(500, { code: "internal", message: "no more answers" });
    },
  });
  // The app's defaults (providers.tsx), without retries.
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: 30_000, refetchOnWindowFocus: false, retry: false } } });
  const show = (ids: string[]): ReactNode => (
    <QueryClientProvider client={client}>
      <Names api={api} ids={ids} />
    </QueryClientProvider>
  );
  return { requests, show, client };
}

const shown = () => screen.getAllByRole("listitem").map((li) => li.textContent);

test("someone who joined after the list was loaded is fetched, so their name shows", async () => {
  const { show } = setup(json(200, [ana]), json(200, [ana, raj]));
  const { rerender } = render(show([ana.id]));
  await screen.findByText("Ana");

  rerender(show([ana.id, raj.id])); // Raj's first comment arrives
  expect(await screen.findByText("Raj")).toBeTruthy();
});

test("a failed fetch keeps the names it had", async () => {
  const { show, requests, client } = setup(json(200, [ana]), json(503, { code: "unavailable", message: "down" }));
  const { rerender } = render(show([ana.id]));
  await screen.findByText("Ana");

  rerender(show([ana.id, raj.id]));
  await waitFor(() => expect(requests.length).toBeGreaterThanOrEqual(2));
  await waitFor(() => expect(client.isFetching()).toBe(0));
  expect(shown()).toEqual(["Ana", "Someone"]);
});

test("each id the list doesn't know is fetched for once, not on every render", async () => {
  const unknown = "01a0ff4b-6485-7357-ac78-9da028c90009";
  const { show, requests, client } = setup(json(200, [ana]), json(200, [ana]), json(200, [ana]), json(200, [ana]));
  const { rerender } = render(show([ana.id, unknown]));
  await waitFor(() => expect(requests).toHaveLength(2)); // the load, then one more for the id it lacks
  await waitFor(() => expect(client.isFetching()).toBe(0));

  // Every event re-renders with a fresh array of ids.
  rerender(show([ana.id, unknown]));
  rerender(show([ana.id, unknown]));
  await waitFor(() => expect(client.isFetching()).toBe(0));
  expect(requests).toHaveLength(2);
  expect(shown()).toEqual(["Ana", "Someone"]);
});
