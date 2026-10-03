# Performance and scale

This is the evidence for the brief's Option 1, Performance & Scale: three load tests with their results, and the index strategy behind the queries they exercise.

Every measured number on this page is copied from a file in [`loadtest/results/`](../loadtest/results/). Test settings, such as how many virtual users run and for how long, come from the scripts in [`loadtest/`](../loadtest/).

## Environment

- **Machine:** an Apple M1 Pro (8 cores, 16 GB), with the stack in Docker Desktop 4.12's VM (4 CPUs, 7.7 GB).
- **Stack:** one API instance, plus Postgres 17 and Redis 8, all from `docker compose up`.
- **Load generator:** k6, on the same Mac as the stack, so it competes with the Docker VM for CPU.
- **Dataset for the read test and the query plans:** the seeded demo project, with 10,000 tasks, 1,500 comments, 21,560 events and 3,641 dependencies.
- **Write and fan-out tests:** they create their own projects, named `loadtest-…`.

## Load tests

| Test | What it does | Thresholds | Result |
| ---- | ------------ | ---------- | ------ |
| [Write throughput](#write-throughput) | 50 writers on one project for 60 s, then 50 writers spread over 20 projects for 60 s | p95 under 300 ms (one project) and 150 ms (spread); under 1% failed | All passed |
| [Fan-out](#fan-out) | 200 people watching one project while one person makes 10 changes a second for 45 s | delivery p95 under 250 ms and p99 under 500 ms; 0 seq gaps; every listener caught up | p99 failed in the recorded run; everything else passed |
| [Opening a project](#opening-a-project) | 10 people opening the 10,000-task project over and over for 30 s | a full open's p95 under 2000 ms; every open gets every task; 0 failed | All passed |

### Write throughput

[`writes.js`](../loadtest/writes.js) · results: [`writes.md`](../loadtest/results/writes.md)

Every change starts with `UPDATE projects SET seq = seq + 1`, which locks the project's row until the change commits. That lock is what gives each project one gapless, ordered event log. It also means changes to one project apply one at a time, so the test measures what that costs:

| | Changes | Rate | A change: p50 | p95 | p99 |
| --- | --- | --- | --- | --- | --- |
| 50 writers, one project | 28,963 | 482.7/s | 98.0 ms | 135.2 ms | 200.4 ms |
| 50 writers, 20 projects | 53,949 | 899.1/s | 52.5 ms | 73.0 ms | 114.2 ms |

- **One project caps throughput.** On this machine, one project takes 482.7 changes a second, each waiting its turn on the lock.
- **Spreading removes that wait.** The same 50 writers spread over 20 projects reach 899.1 changes a second, and each change takes about half as long, because writes to different projects don't wait for each other.
- **No conflicts.** Requests failed: 0 of 83,534. Each virtual user changes only its own tasks, so status moves never collide, and any 409 would have been a real error. A deliberate run where users shared one task failed this threshold, which proves the check works.

### Fan-out

[`fanout.js`](../loadtest/fanout.js) · results: [`fanout.md`](../loadtest/results/fanout.md), reruns [`fanout-rerun-1.md`](../loadtest/results/fanout-rerun-1.md) and [`fanout-rerun-2.md`](../loadtest/results/fanout-rerun-2.md)

Each change's title is the time the writer sent it. A delivery is measured from just before the writer sends the change until a listener receives its event, so it includes the write itself.

| Run | Deliveries | p50 | p95 | p99 | max | Writer's own request p99 |
| --- | --- | --- | --- | --- | --- | --- |
| Recorded run | 89,400 | 23.0 ms | 89.0 ms | **669.0 ms** | 942.0 ms | 412.4 ms |
| Rerun 1, a minute later | | 18.0 ms | 36.0 ms | 45.0 ms | 77.0 ms | 32.8 ms |
| Rerun 2, straight after another full write test | | 17.0 ms | 37.0 ms | 47.0 ms | 80.0 ms | 34.1 ms |

- **Order and completeness held in every run.** There were 0 seq gaps: every listener got every event, in order. In the recorded run 200 of 200 listeners caught up, and 0 of 451 requests failed.
- **The recorded run failed its p99 threshold** (669.0 ms against 500 ms). Its p95, 89.0 ms, passed.
- **The slow part was the writes, not the fan-out.** The writer's own requests had the same tail in that run (p99 412.4 ms, max 736.5 ms), and every delivery includes its write's time.
- **The cause wasn't found.** Postgres was running a scheduled checkpoint at the time, but rerun 1 ran during that same checkpoint and passed. Rerun 2 deliberately repeated the conditions, starting straight after a full write test, and also passed.
- **The result stands.** It's recorded as it happened, and the threshold stays where it is.

### Opening a project

[`initial-load.js`](../loadtest/initial-load.js) · results: [`initial-load.md`](../loadtest/results/initial-load.md)

Opening a project loads every task summary, page by page, gzipped, as the web app does.

| | p50 | p95 | p99 | max |
| --- | --- | --- | --- | --- |
| A full open: every summary | 411.0 ms | 502.0 ms | 583.8 ms | 673.0 ms |
| One page of 500 summaries | 18.2 ms | 30.7 ms | 39.9 ms | 97.2 ms |

- **Opens:** 723 in 30 seconds by 10 people at once. All 723 received every one of the 10,000 tasks, and 0 of 14,482 requests failed.
- **On the wire:** the summaries are 2,586,735 bytes of JSON, but only 393,233 bytes per open are received with gzip, headers included.
- **Heavy fields aren't in this load:** descriptions, custom fields and comments load only when a task is opened.

## Index strategy

[`explain.sql`](../loadtest/explain.sql) runs `EXPLAIN (ANALYZE, BUFFERS)` on every hot query against the seeded project. The full plans are in [`explain.txt`](../loadtest/results/explain.txt).

- **No full-table scans:** every query is served by an index, with no Seq Scan on any table.
- **A plan for a write without writing:** `NextProjectSeq` runs inside a transaction that is rolled back. `projects.seq` was 21,560 before and after.

| Query | Used for | Index | Plan | Time |
| ----- | -------- | ----- | ---- | ---- |
| `ListTaskSummaries` | One page of 500 summaries | `tasks (project_id, id)`; each task's dependencies from `task_dependencies`' primary key | Index Scan, 500 rows; Index Only Scan per row | 0.691 ms (first page), 1.092 ms (a middle page) |
| `ListEvents` | Catching up after a reconnect | `events` primary key `(project_id, seq)` | Index Scan, 1,000 events | 0.617 ms |
| `ListComments` | A task's thread | `comments (task_id, seq)` | Bitmap Index Scan | 0.064 ms |
| `GetTaskDetail` | Opening a task | `tasks` primary key | Index Scan | 0.044 ms |
| `DependencyStatuses` | The Done rule | `task_dependencies` primary key, then `tasks` primary key | Nested Loop of index scans | 0.034 ms |
| `DependentIDs` | Who depends on a task (delete) | `task_dependencies (depends_on_id)` | Index Scan | 0.046 ms |
| `DependencyWouldCycle` | Rejecting a dependency loop | `task_dependencies` primary key `(task_id, depends_on_id)` | Recursive Union of Index Only Scans | 0.119 ms |
| `EventByChangeID` | Spotting a retried change | `events (change_id)`, unique | Index Scan | 0.060 ms |
| `NextProjectSeq` | Every write: next seq, row lock | `projects` primary key | Index Scan, then Update | 0.294 ms |

**Why these indexes:**
- **Matched to the queries:** each index leads with exactly the columns its query filters and orders by.
- **Keyset paging:** task pages (`id > cursor`) and the event log (`seq > n`) never use `OFFSET`, so a page deep into the project is still one index range scan, not a walk past every row before it.
- **Free ordering:** the events primary key is also the log's order, so catching up is one range scan.

**The data is modest per task:** the longest thread has 3 comments, and the cycle walk visits 6 rows. So these times show each access path more than worst-case sizes. What scales is the path itself: each query is an index lookup whose cost grows with the rows it returns, not with the size of the project.

**Indexes deliberately left out:**
- **`tasks.tags`, `tasks.assignees` (GIN), `tasks.status`:** filtering and Blocked are computed in the browser over the loaded summaries, by design, so the database never filters on them.
- **`projects.created_at`:** the project list is small and read in one query.
- **`comments.author_id`, `events.actor_id`:** users are never deleted, and nothing looks rows up by who wrote them.

**As it grows:**
- **Partition `events` by `hash(project_id)`:** keeps each project's log and its index pages together, and bounds index size.
- **Snapshot and compact old events:** a client more than 5,000 events behind already reloads the project instead of replaying, so old events only serve as history.
- **Add BRIN on `events.created_at`** if queries by time range ever appear.
- **Serve the event reads from a read replica,** as the README's scaling notes describe.

## Reproduce

From the repo root, with k6 installed (`brew install k6`):

```sh
docker compose up --build -d --wait

k6 run loadtest/writes.js
ulimit -n 4096                    # fanout.js holds 200 WebSockets; macOS defaults to 256 open files
k6 run loadtest/fanout.js

docker compose exec api /seed     # ends with the project's URL; its last part is the id
k6 run -e PROJECT_ID=<id> loadtest/initial-load.js

docker compose exec -T postgres psql -U app -d tasks -v project_id=<id> -f - \
  < loadtest/explain.sql > loadtest/results/explain.txt
```

Options:
- **`-e SMOKE=1`:** runs a script in seconds, to check it works.
- **`-e ENVIRONMENT="…"`:** records the machine in the results.
- **`-e API_URL=…`:** points the scripts at another API.

Each script writes its results to `loadtest/results/` (`<name>.md`, plus k6's full summary in `<name>.json`).

Everything the tests create stays in the local database, named `loadtest-…`. `docker compose down -v` deletes all local data, including your own projects.
