-- How Postgres runs every hot query, on a seeded project:
--
--   docker compose exec -T postgres psql -U app -d tasks -v project_id=<id> -f - \
--     < loadtest/explain.sql > loadtest/results/explain.txt
--
-- Each query is copied from api/db/queries/*.sql, with literal parameters
-- picked so it does real work. EXPLAIN ANALYZE runs the query; the one write
-- runs inside a transaction that is rolled back.
\set ON_ERROR_STOP on
ANALYZE;

\echo 'The project the plans run on:'
SELECT to_char((SELECT count(*) FROM tasks WHERE project_id = :'project_id'), 'FM999,999,999') AS tasks,
       to_char((SELECT count(*) FROM comments WHERE project_id = :'project_id'), 'FM999,999,999') AS comments,
       to_char((SELECT count(*) FROM events WHERE project_id = :'project_id'), 'FM999,999,999') AS events,
       to_char((SELECT count(*) FROM task_dependencies WHERE project_id = :'project_id'), 'FM999,999,999') AS dependencies;

-- The 5,000th task by id: a page from the middle of the project.
SELECT id AS mid_task FROM tasks WHERE project_id = :'project_id' ORDER BY id OFFSET 4999 LIMIT 1 \gset
-- The task with the most dependencies, and the one most depended on.
SELECT task_id AS most_deps FROM task_dependencies WHERE project_id = :'project_id'
  GROUP BY task_id ORDER BY count(*) DESC, task_id LIMIT 1 \gset
SELECT depends_on_id AS most_depended FROM task_dependencies WHERE project_id = :'project_id'
  GROUP BY depends_on_id ORDER BY count(*) DESC, depends_on_id LIMIT 1 \gset
-- The task with the most comments.
SELECT task_id AS most_comments FROM comments WHERE project_id = :'project_id'
  GROUP BY task_id ORDER BY count(*) DESC, task_id LIMIT 1 \gset
-- For the cycle check: dependencies always point at older tasks, so the
-- newest task with dependencies walks furthest upstream, looking for the
-- oldest task. (UUIDv7 ids sort by creation.)
SELECT task_id AS cycle_from FROM task_dependencies WHERE project_id = :'project_id' ORDER BY task_id DESC LIMIT 1 \gset
SELECT id AS cycle_to FROM tasks WHERE project_id = :'project_id' ORDER BY id LIMIT 1 \gset
-- The middle of the event log, and one change id from it.
SELECT seq / 2 AS mid_seq FROM projects WHERE id = :'project_id' \gset
SELECT change_id FROM events WHERE project_id = :'project_id' AND seq = :mid_seq \gset

\echo '== ListTaskSummaries: the first page, then a page from the middle'
EXPLAIN (ANALYZE, BUFFERS)
SELECT t.id, t.title, t.status, t.priority, t.assignees, t.tags, t.updated_seq,
       ARRAY(
           SELECT d.depends_on_id FROM task_dependencies d
           WHERE d.task_id = t.id ORDER BY d.depends_on_id
       )::uuid[] AS dependencies
FROM tasks t
WHERE t.project_id = :'project_id' AND t.id > '00000000-0000-0000-0000-000000000000'
ORDER BY t.id
LIMIT 500;
EXPLAIN (ANALYZE, BUFFERS)
SELECT t.id, t.title, t.status, t.priority, t.assignees, t.tags, t.updated_seq,
       ARRAY(
           SELECT d.depends_on_id FROM task_dependencies d
           WHERE d.task_id = t.id ORDER BY d.depends_on_id
       )::uuid[] AS dependencies
FROM tasks t
WHERE t.project_id = :'project_id' AND t.id > :'mid_task'
ORDER BY t.id
LIMIT 500;

\echo '== ListEvents: 1,000 events from the middle of the log'
EXPLAIN (ANALYZE, BUFFERS)
SELECT seq, change_id, actor_id, created_at, touches, payload
FROM events
WHERE project_id = :'project_id' AND seq > :mid_seq
ORDER BY seq
LIMIT 1000;

\echo '== ListComments: the longest thread'
EXPLAIN (ANALYZE, BUFFERS)
SELECT id, task_id, author_id, content, seq, created_at
FROM comments
WHERE task_id = :'most_comments' AND seq > 0
ORDER BY seq
LIMIT 50;

\echo '== GetTaskDetail'
EXPLAIN (ANALYZE, BUFFERS)
SELECT t.id, t.project_id, t.title, t.status, t.priority, t.description, t.tags,
       t.custom_fields, t.assignees, t.updated_seq,
       ARRAY(
           SELECT d.depends_on_id FROM task_dependencies d
           WHERE d.task_id = t.id ORDER BY d.depends_on_id
       )::uuid[] AS dependencies
FROM tasks t
WHERE t.id = :'mid_task';

\echo '== DependencyStatuses: the task with the most dependencies'
EXPLAIN (ANALYZE, BUFFERS)
SELECT t.status
FROM task_dependencies d
JOIN tasks t ON t.id = d.depends_on_id
WHERE d.task_id = :'most_deps';

\echo '== DependentIDs: the most depended-on task'
EXPLAIN (ANALYZE, BUFFERS)
SELECT task_id FROM task_dependencies WHERE depends_on_id = :'most_depended' ORDER BY task_id;

\echo '== DependencyWouldCycle: from the newest task with dependencies to the oldest task'
EXPLAIN (ANALYZE, BUFFERS)
WITH RECURSIVE upstream (id) AS (
    SELECT d.depends_on_id
    FROM task_dependencies d
    WHERE d.task_id = :'cycle_from'::uuid
  UNION
    SELECT d.depends_on_id
    FROM task_dependencies d
    JOIN upstream u ON d.task_id = u.id
)
SELECT EXISTS (
    SELECT 1 FROM upstream WHERE id = :'cycle_to'::uuid
) AS would_cycle;

\echo '== EventByChangeID'
EXPLAIN (ANALYZE, BUFFERS)
SELECT project_id, seq FROM events WHERE change_id = :'change_id';

\echo '== NextProjectSeq: inside a transaction that is rolled back'
BEGIN;
EXPLAIN (ANALYZE, BUFFERS)
UPDATE projects SET seq = seq + 1 WHERE id = :'project_id' RETURNING seq;
ROLLBACK;
