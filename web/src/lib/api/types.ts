// Friendly names for the types generated from contract/openapi.yaml.
// Regenerate schema.d.ts with `npm run gen:api` after changing the contract.
import type { components } from "./schema";

type Schemas = components["schemas"];

export type Status = Schemas["Status"];
export type Priority = Schemas["Priority"];
export type User = Schemas["User"];
export type Project = Schemas["Project"];
export type ProjectSummary = Schemas["ProjectSummary"];
export type TaskSummary = Schemas["TaskSummary"];
export type TaskPage = Schemas["TaskPage"];
export type Task = Schemas["Task"];
export type CustomFieldValue = Schemas["CustomFieldValue"];
export type Comment = Schemas["Comment"];
export type CommentPage = Schemas["CommentPage"];
/** An event of a project's log. (Named so it doesn't clash with the DOM's Event.) */
export type LogEvent = Schemas["Event"];
export type Change = Schemas["Change"];
/** The change with the given type, e.g. ChangeOf<"task.update">. */
export type ChangeOf<T extends Change["type"]> = Extract<Change, { type: T }>;
export type ErrorCode = Schemas["ErrorCode"];
export type ApiError = Schemas["Error"];
export type WsClientMessage = Schemas["WsClientMessage"];
export type WsServerMessage = Schemas["WsServerMessage"];
