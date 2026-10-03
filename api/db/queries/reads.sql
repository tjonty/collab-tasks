-- Queries for the read endpoints. Task lists page by id (a keyset cursor,
-- not an offset), so writes during paging can't shift a page.

-- name: ListUsers :many
SELECT id, display_name FROM users ORDER BY display_name, id;

-- name: ListProjects :many
SELECT id, name, description, seq FROM projects ORDER BY created_at, id;

-- name: ProjectSeq :one
SELECT seq FROM projects WHERE id = $1;

-- name: ListTaskSummaries :many
-- One page of a project's tasks after the cursor, without the heavy fields.
-- Served by the (project_id, id) unique index.
SELECT t.id, t.title, t.status, t.priority, t.assignees, t.tags, t.updated_seq,
       ARRAY(
           SELECT d.depends_on_id FROM task_dependencies d
           WHERE d.task_id = t.id ORDER BY d.depends_on_id
       )::uuid[] AS dependencies
FROM tasks t
WHERE t.project_id = @project_id AND t.id > @after
ORDER BY t.id
LIMIT @page_size;

-- name: GetTaskDetail :one
SELECT t.id, t.project_id, t.title, t.status, t.priority, t.description, t.tags,
       t.custom_fields, t.assignees, t.updated_seq,
       ARRAY(
           SELECT d.depends_on_id FROM task_dependencies d
           WHERE d.task_id = t.id ORDER BY d.depends_on_id
       )::uuid[] AS dependencies
FROM tasks t
WHERE t.id = $1;

-- name: ListComments :many
-- Served by the (task_id, seq) index.
SELECT id, task_id, author_id, content, seq, created_at
FROM comments
WHERE task_id = @task_id AND seq > @after_seq
ORDER BY seq
LIMIT @page_size;

-- name: ListEvents :many
SELECT seq, change_id, actor_id, created_at, touches, payload
FROM events
WHERE project_id = @project_id AND seq > @after_seq
ORDER BY seq
LIMIT @page_size;

-- name: ProjectSeqs :many
-- The latest seq of each listed project, for the realtime poll.
SELECT id, seq FROM projects WHERE id = ANY(@ids::uuid[]);
