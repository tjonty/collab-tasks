// The typed HTTP client, generated from contract/openapi.yaml.
import createClient from "openapi-fetch";

import type { PostOutcome } from "@/lib/sync/changes";
import type { ProjectApi } from "@/lib/sync/store";

import type { paths } from "./schema";
import type { ErrorCode } from "./types";

export interface ApiOptions {
  baseUrl: string;
  /** The current user's id, sent as X-User-Id. */
  userId: () => string | null;
  /** The server doesn't know that id, e.g. because the database was reset. */
  onUnknownUser: () => void;
  fetch?: (req: Request) => Promise<Response>;
}

export function createApi(opts: ApiOptions) {
  const client = createClient<paths>({ baseUrl: opts.baseUrl, fetch: opts.fetch });
  client.use({
    onRequest({ request }) {
      const id = opts.userId();
      if (id) request.headers.set("X-User-Id", id);
      return request;
    },
    onResponse({ response }) {
      if (response.status === 401) opts.onUnknownUser();
      return response;
    },
  });
  return client;
}

export type ApiClient = ReturnType<typeof createApi>;

/** A failed request, carrying the server's {code, message}. */
export class ApiFailure extends Error {
  constructor(
    readonly status: number,
    readonly code: ErrorCode,
    message: string,
  ) {
    super(message);
  }
}

function failure(status: number, error: { code?: ErrorCode; message?: string } | undefined): ApiFailure {
  return new ApiFailure(status, error?.code ?? "internal", error?.message ?? `The API answered ${status}`);
}

/** The calls ProjectSync makes, over HTTP. */
export function projectApi(client: ApiClient): ProjectApi {
  return {
    async getProject(projectId) {
      const { data, error, response } = await client.GET("/projects/{projectId}", { params: { path: { projectId } } });
      if (data) return data;
      throw failure(response.status, error);
    },

    async listTasks(projectId, after) {
      const { data, error, response } = await client.GET("/projects/{projectId}/tasks", {
        params: { path: { projectId }, query: { after } },
      });
      if (data) return data;
      throw failure(response.status, error);
    },

    async getTask(taskId) {
      const { data, error, response } = await client.GET("/tasks/{taskId}", { params: { path: { taskId } } });
      if (data) return data;
      if (response.status === 404) return null;
      throw failure(response.status, error);
    },

    async listComments(taskId, afterSeq) {
      const { data, error, response } = await client.GET("/tasks/{taskId}/comments", {
        params: { path: { taskId }, query: { afterSeq } },
      });
      if (data) return data;
      if (response.status === 404) return null;
      throw failure(response.status, error);
    },

    async postChange(projectId, changeId, change): Promise<PostOutcome> {
      try {
        const { data, error, response } = await client.POST("/projects/{projectId}/changes", {
          params: { path: { projectId } },
          body: { changeId, change },
        });
        if (data) return { kind: "ok", seq: data.seq };
        if (response.status >= 500) return { kind: "retry", reason: `HTTP ${response.status}` };
        const f = failure(response.status, error);
        return { kind: "rejected", code: f.code, message: f.message };
      } catch (err) {
        // The request never got an answer: retrying with the same changeId is safe.
        return { kind: "retry", reason: String(err) };
      }
    },
  };
}
