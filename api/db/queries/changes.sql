-- Queries for the write pipeline. Every change runs them inside one
-- transaction that starts with NextProjectSeq.

-- name: NextProjectSeq :one
-- Reserves the project's next seq and locks its row until the transaction
-- ends, so changes to one project apply one at a time, in seq order, and
-- every rule check sees the latest committed state.
UPDATE projects SET seq = seq + 1 WHERE id = $1 RETURNING seq;

-- name: EventByChangeID :one
SELECT project_id, seq FROM events WHERE change_id = $1;

-- name: InsertEvent :exec
INSERT INTO events (project_id, seq, change_id, type, entity_id, payload, actor_id, touches)
VALUES (@project_id, @seq, @change_id, @type, @entity_id, @payload, @actor_id, @touches::uuid[]);

-- name: GetProject :one
SELECT id, name, description, metadata, seq FROM projects WHERE id = $1;

-- name: SaveProject :exec
UPDATE projects
SET name = @name, description = @description, metadata = @metadata, updated_at = now()
WHERE id = @id;

-- name: GetTask :one
SELECT id, title, status, priority, description, tags, custom_fields, assignees
FROM tasks
WHERE project_id = $1 AND id = $2;

-- name: TaskIDExists :one
SELECT EXISTS (SELECT 1 FROM tasks WHERE id = $1);

-- name: InsertTask :exec
INSERT INTO tasks (id, project_id, title, priority, description, tags, custom_fields, assignees)
VALUES (@id, @project_id, @title, @priority, @description, @tags::text[], @custom_fields, @assignees::uuid[]);

-- name: SaveTask :exec
UPDATE tasks
SET title = @title, status = @status, priority = @priority, description = @description,
    tags = @tags::text[], custom_fields = @custom_fields, assignees = @assignees::uuid[],
    updated_at = now()
WHERE project_id = @project_id AND id = @id;

-- name: TouchTasks :exec
-- Stamps every task a change wrote with the change's seq.
UPDATE tasks SET updated_seq = @seq, updated_at = now()
WHERE project_id = @project_id AND id = ANY(@ids::uuid[]);

-- name: DeleteTask :exec
DELETE FROM tasks WHERE project_id = $1 AND id = $2;

-- name: CountProjectTasks :one
SELECT count(*) FROM tasks WHERE project_id = @project_id AND id = ANY(@ids::uuid[]);

-- name: DependentIDs :many
-- The tasks that depend on depends_on_id.
SELECT task_id FROM task_dependencies WHERE depends_on_id = $1 ORDER BY task_id;

-- name: DependencyStatuses :many
-- The statuses of the tasks that task_id depends on.
SELECT t.status
FROM task_dependencies d
JOIN tasks t ON t.id = d.depends_on_id
WHERE d.task_id = $1;

-- name: InsertDependency :exec
INSERT INTO task_dependencies (project_id, task_id, depends_on_id)
VALUES (@project_id, @task_id, @depends_on_id)
ON CONFLICT DO NOTHING;

-- name: DeleteDependency :exec
DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on_id = $2;

-- name: CommentIDExists :one
SELECT EXISTS (SELECT 1 FROM comments WHERE id = $1);

-- name: InsertComment :exec
INSERT INTO comments (id, task_id, project_id, author_id, content, seq)
VALUES (@id, @task_id, @project_id, @author_id, @content, @seq);

-- name: CountUsers :one
SELECT count(*) FROM users WHERE id = ANY(@ids::uuid[]);
