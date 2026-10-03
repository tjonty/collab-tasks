# Collab Tasks

A real-time collaborative task manager: projects, tasks with dependencies and a status workflow, and comment threads. Every open tab sees each change within a second. A Go API and a Next.js web app, backed by Postgres and Redis, with no managed real-time database.

## Quick start

### Prerequisites

- Docker with Compose v2 or later.
- For local development only: Go 1.27 and Node 24.

### Run everything

```sh
docker compose up --build
```

This starts Postgres, Redis, the API and the web app. Then open:

- Web: http://localhost:3000. Pick a display name, then create or open a project. Each browser tab can be a different person.
- API: http://localhost:8080/healthz. It returns `{"postgres":"ok","redis":"ok"}` with 200, or 503 if either dependency is down.

### Seed a demo project

With the stack running, create a project of 10,000 tasks, with dependencies, comments and every status, through the same write pipeline the API uses. It takes about 30 seconds and prints a link to the project:

```sh
docker compose exec api /seed
```

Flags: `-tasks`, `-comments` and `-random-seed` (the same seed plans the same project).

### Local development

Keep Postgres and Redis in Docker, and run the API and web app on your machine with hot reload:

```sh
docker compose up -d postgres redis   # if they are not already running
docker compose stop api web           # free ports 8080 and 3000
```

Then, in two terminals:

```sh
cd api && go run ./cmd/server
```

```sh
cd web && npm install && npm run dev
```

When you're done, stop both with Ctrl-C and bring the containers back:

```sh
docker compose start api web
```

The API reads `PORT`, `DATABASE_URL`, `REDIS_URL` and `CORS_ORIGIN`. Their defaults match the Docker setup, so no `.env` file is needed. The web app's API URL comes from `NEXT_PUBLIC_API_URL`, default `http://localhost:8080`. That value is built into the bundle, so changing it means rebuilding (`docker compose build web`).

### Ports

| Port | Service    |
| ---- | ---------- |
| 3000 | Web app    |
| 8080 | API        |
| 5432 | Postgres   |
| 6379 | Redis      |

## Tests

```sh
cd api && go test ./...      # unit and integration tests (Docker must be running; -short skips the Docker ones)
cd web && npm test           # Vitest: the sync engine, the task rules and the UI components
cd web && npm run test:e2e   # Playwright: two people, two browsers, against the running stack
```

`npm run test:e2e:demo` runs the end-to-end test in visible, slowed-down browsers and records a video of each in `web/test-results/`. It doubles as the demo script.

## Try it

1. Open http://localhost:3000 in two windows side by side. Each tab is its own person, so pick two names. Create a project in one window and open the same URL in the other.
2. Add tasks, comment, link dependencies and change statuses. Each change shows up in the other window within a second.
3. Try to mark a task Done while one of its dependencies is open. Done is disabled, and the drawer says what it's waiting on.
4. Run `docker compose stop redis`. Changes still arrive, within about 5 seconds. Bring it back with `docker compose start redis`.
5. Take one window offline (DevTools → Network → Offline), make edits in the other, then go back online. The offline window catches up.
6. Seed a demo project and scroll through its 10,000 tasks.
7. Read the event log. Every edit is a small event that carries only what changed. Reads need a user id, which `POST /users` returns:

   ```sh
   curl -s localhost:8080/users -H 'Content-Type: application/json' -d '{"displayName":"curl"}'
   curl -s 'localhost:8080/projects/<project-id>/events?after=0&limit=5' -H 'X-User-Id: <id from above>'
   ```

   The project id is in the page URL.

## How it works

**One ordered log per project.**
- Every write is a typed change, such as `task.setStatus`, `task.addDependency` or `comment.create`; there are 12 types. It's sent to `POST /projects/{id}/changes`.
- Each change runs in one Postgres transaction that:
  1. locks the project row and takes its next sequence number (`seq`)
  2. checks the rules against the latest state
  3. applies the change
  4. appends one event to the log
- A rejected change rolls back, so `seq` has no gaps. A client that sees a jump knows it missed something and asks again.
- Each change carries a client-generated `changeId`. Retrying it returns the original `seq` instead of applying it twice.

**Doorbell and pull.**
- After a commit, the API publishes a tiny `{projectId, seq}` on Redis. Every API instance listens.
- For each project that has viewers, the instance reads the new events from Postgres, in order, and pushes them over the WebSocket.
- Postgres is the only source of order, so a lost or duplicated doorbell does no harm. A 5-second poll catches anything a doorbell missed, including while Redis is down.
- There is no managed real-time database: Postgres holds the state and the log, Redis carries only doorbells, and the WebSocket server is our own.

**Catching up.**
- A tab subscribes from the last `seq` it has. The server replays everything after it, then streams live events, with no gaps and no duplicates.
- After a dropped connection, the tab resubscribes the same way.
- A tab more than 5,000 events behind reloads the project instead.

**Large projects (2 MB+).** Nothing ever resends a whole project.
- Opening a project loads compact task summaries in the background while the first page renders. A summary is about 250 bytes, and a page of 500 is about 20 KB gzipped.
- Descriptions, custom fields and comments load when a task is opened.
- An edit travels as one event that carries only what changed. A test builds a task over 10 KB and checks that every kind of edit to it is a single event under 1 KB.
- Filtering, sorting and Blocked are computed in the browser, and the list is virtualized.

**Merging.**
- Every task records the `seq` of its last change, and every event lists the tasks it touched.
- An event applies to a task only if it's newer, so pages and events can arrive in any order and still end up the same. A randomized test checks this.
- Deleted tasks are remembered for the session, so a page fetched before the delete can't bring one back.

## Decisions and tradeoffs

- **Simultaneous edits:**
  - The last write wins, per field, in the server's order. Custom fields merge per key.
  - Assignees, tags and dependencies are added or removed one at a time, so two people adding different items both succeed.
  - Status changes are never overwritten blindly. Each one is checked against the current status and can be rejected, with a message saying why.
- **Known tradeoff:** if two people save the same description at the same moment, the later save replaces the whole text. There is no character-level merging.
- **Dependencies:**
  - Work can start any time, but a task can become Done only when all its dependencies are Done.
  - Cycles are rejected. The check runs under the project lock, so two opposite links racing can't both succeed.
  - Blocked is computed, never stored. Reopening a dependency doesn't undo Done.
- **Server components render only the static shell.** Project data lives in client components backed by the sync store. Anything rendered on the server would be stale a moment later, and the live stream has to start from a known `seq` anyway.
- **Saving, not optimistic updates:** a changed field shows "saving…" until its event comes back.
- **Failed saves:** a save that fails on the network retries with the same `changeId`, then offers Retry or Discard.

## Not included

- **Identity is not secure.** There's no login. Picking a display name creates a user, and the API trusts the `X-User-Id` header (`?userId=` on the WebSocket). Anyone who knows a user's id can act as that user. That's a demo shortcut, not something to ship.
- **No deleting projects, comments or users.** The brief doesn't ask for it. Deleting a task deletes its comments and its dependency links too. Comments can't be edited.

## Scaling

- Run more API instances behind a load balancer. No sticky sessions are needed: every instance hears every doorbell and reads from the same Postgres.
- As instances multiply, move from each one subscribing to every project (`PSUBSCRIBE project:*`) to per-project subscriptions or NATS subjects, so an instance only hears about the projects its clients have open.
- Serve the event reads from a Postgres read replica.

## Code map

| Path | What's there |
| ---- | ------------ |
| `contract/openapi.yaml` | The API contract, including the WebSocket messages. The Go server types and the TypeScript types are generated from it. |
| `api/internal/domain` | The status workflow, the Done rule and field limits, in pure Go |
| `api/internal/changes` | The write pipeline: one change, one transaction, one event |
| `api/internal/httpapi` | The HTTP handlers, plus identity, CORS and gzip |
| `api/internal/realtime` | The WebSocket hub, the Redis doorbell, per-project feeds and the poll |
| `api/db`, `api/internal/store` | Migrations (goose) and SQL queries (sqlc) |
| `api/internal/seed`, `api/cmd/seed` | The demo project generator |
| `web/src/lib/sync` | The client sync engine: the store, applying events, the WebSocket connection and sending changes |
| `web/src/lib/domain` | The workflow rules, mirrored from Go so the UI can explain them |
| `web/src/components` | The UI |
| `web/e2e` | The two-person Playwright test |
