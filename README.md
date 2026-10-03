# Collab Tasks

A Go API and a Next.js web app, backed by Postgres and Redis.

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
