import { describe, expect, test, vi } from "vitest";

import { createApi, projectApi } from "./client";

const P = "01a0ff4b-6485-7357-ac78-9da028c907f6";
const T = "01a0ff4b-6485-7357-ac78-9da028c907f7";
const U = "01a0ff4b-6485-7357-ac78-9da028c907f8";

/** A fetch that answers every request with the next scripted response. */
function fakeFetch(...answers: (Response | Error)[]) {
  const requests: Request[] = [];
  const fetch = async (req: Request) => {
    requests.push(req);
    const next = answers.shift() ?? new Response(null, { status: 500 });
    if (next instanceof Error) throw next;
    return next;
  };
  return { fetch, requests };
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function setup(...answers: (Response | Error)[]) {
  const { fetch, requests } = fakeFetch(...answers);
  const onUnknownUser = vi.fn();
  const client = createApi({ baseUrl: "http://api.test", userId: () => U, onUnknownUser, fetch });
  return { api: projectApi(client), client, requests, onUnknownUser };
}

describe("the API client", () => {
  test("sends X-User-Id to the API's base URL", async () => {
    const { api, requests } = setup(json(200, { tasks: [], nextCursor: null, seq: 3 }));
    await api.listTasks(P, T);
    expect(requests[0].url).toBe(`http://api.test/projects/${P}/tasks?after=${T}`);
    expect(requests[0].headers.get("X-User-Id")).toBe(U);
  });

  test("a 401 means the server doesn't know this user", async () => {
    const { client, onUnknownUser } = setup(json(401, { code: "unknown_user", message: "who?" }));
    await client.GET("/users");
    expect(onUnknownUser).toHaveBeenCalledTimes(1);
  });

  test("getTask and listComments return null for a task that's gone", async () => {
    const gone = () => json(404, { code: "task_not_found", message: "gone" });
    const { api } = setup(gone(), gone());
    expect(await api.getTask(T)).toBeNull();
    expect(await api.listComments(T, 0)).toBeNull();
  });

  test("getProject throws with the server's message when it fails", async () => {
    const { api } = setup(json(404, { code: "project_not_found", message: `project ${P} does not exist` }));
    await expect(api.getProject(P)).rejects.toThrow(`project ${P} does not exist`);
  });

  describe("postChange", () => {
    const change = { type: "task.delete" as const, taskId: T };

    test("posts the change with its id and returns the seq", async () => {
      const { api, requests } = setup(json(200, { seq: 7 }));
      expect(await api.postChange(P, "c1", change)).toEqual({ kind: "ok", seq: 7 });
      expect(requests[0].method).toBe("POST");
      expect(await requests[0].json()).toEqual({ changeId: "c1", change });
    });

    test("a 409 or 422 is a rejection: retrying won't help", async () => {
      const { api } = setup(
        json(409, { code: "dependency_cycle", message: "loop" }),
        json(422, { code: "validation_failed", message: "title: is required" }),
      );
      expect(await api.postChange(P, "c1", change)).toEqual({ kind: "rejected", code: "dependency_cycle", message: "loop" });
      expect(await api.postChange(P, "c2", change)).toEqual({
        kind: "rejected", code: "validation_failed", message: "title: is required",
      });
    });

    test("a 5xx or a network failure is worth retrying", async () => {
      const { api } = setup(
        json(503, { code: "unavailable", message: "db down" }),
        new Response("oops", { status: 500 }),
        new TypeError("Failed to fetch"),
      );
      for (let i = 0; i < 3; i++) {
        expect((await api.postChange(P, `c${i}`, change)).kind).toBe("retry");
      }
    });
  });
});
