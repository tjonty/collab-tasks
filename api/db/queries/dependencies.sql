-- name: DependencyWouldCycle :one
-- Reports whether adding "task_id depends on depends_on_id" would close a
-- loop: true when depends_on_id already depends on task_id, directly or
-- indirectly. The walk follows the (task_id, depends_on_id) primary key, and
-- UNION (not UNION ALL) visits each task once.
WITH RECURSIVE upstream (id) AS (
    SELECT d.depends_on_id
    FROM task_dependencies d
    WHERE d.task_id = sqlc.arg(depends_on_id)::uuid
  UNION
    SELECT d.depends_on_id
    FROM task_dependencies d
    JOIN upstream u ON d.task_id = u.id
)
SELECT EXISTS (
    SELECT 1 FROM upstream WHERE id = sqlc.arg(task_id)::uuid
) AS would_cycle;
