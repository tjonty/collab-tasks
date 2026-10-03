// Shared helpers for the k6 load tests: the API calls they make and the
// results files they write. Run the scripts from the repo root against the
// compose stack, e.g. `k6 run loadtest/writes.js`; add `-e SMOKE=1` for a
// tiny run that checks the script itself. Everything they create is named
// `loadtest-…`.
import http from "k6/http";

export const API = __ENV.API_URL || "http://localhost:8080";
export const WS = API.replace(/^http/, "ws");
export const SMOKE = __ENV.SMOKE === "1";

// The trend stats every script reports.
export const TREND_STATS = ["avg", "p(50)", "p(95)", "p(99)", "max"];

function call(method, path, body, userId, name) {
  const headers = { "Content-Type": "application/json" };
  if (userId) headers["X-User-Id"] = userId;
  return http.request(method, API + path, body === undefined ? null : JSON.stringify(body), { headers, tags: { name } });
}

function ok(res, what) {
  if (res.status < 200 || res.status > 299) throw new Error(`${what}: HTTP ${res.status} ${res.body}`);
  return res;
}

export function createUser(name) {
  return ok(call("POST", "/users", { displayName: name }, null, "createUser"), "create user").json();
}

/** Creates a project and returns its id. Any UUID will do as the id. */
export function createProject(userId, name) {
  const id = crypto.randomUUID();
  ok(call("POST", "/projects", { id, name }, userId, "createProject"), "create project");
  return id;
}

export function getProject(userId, projectId) {
  return ok(call("GET", `/projects/${projectId}`, undefined, userId, "getProject"), "get project").json();
}

/** Submits one change; the caller checks the response. */
export function submit(userId, projectId, change) {
  return call("POST", `/projects/${projectId}/changes`, { changeId: crypto.randomUUID(), change }, userId, "submitChange");
}

/** Creates n tasks titled loadtest-task-<i> and returns their ids. */
export function createTasks(userId, projectId, n) {
  const ids = [];
  for (let i = 0; i < n; i++) {
    const id = crypto.randomUUID();
    ok(submit(userId, projectId, { type: "task.create", id, title: `loadtest-task-${i}` }), "create task");
    ids.push(id);
  }
  return ids;
}

/**
 * For handleSummary: a markdown table on stdout and in
 * loadtest/results/<name>.md, and k6's whole summary in <name>.json.
 * Each row is [label, metric, {seconds}?]. A trend shows avg, p50, p95, p99
 * and max in ms; a counter its count and rate (per `seconds` when given,
 * since k6's own rate divides by the whole run); a rate its share and counts.
 * Every threshold is shown with whether it passed; `notes` are lines added
 * under the table.
 */
export function writeSummary(name, data, rows, notes = []) {
  const lines = [
    `# ${name}`,
    "",
    `${SMOKE ? "Smoke run" : "Run"} at ${new Date().toISOString()} against ${API}.`,
    `Environment: ${__ENV.ENVIRONMENT || "not recorded (pass -e ENVIRONMENT=…)"}.`,
    "",
    "| Measure | avg | p50 | p95 | p99 | max | count | rate | Thresholds |",
    "| ------- | --- | --- | --- | --- | --- | ----- | ---- | ---------- |",
    ...rows.map(([label, metric, opts]) => row(label, data.metrics[metric], opts || {})),
    "",
    ...notes.flatMap((note) => [note, ""]),
  ];
  const table = lines.join("\n");
  return {
    stdout: `\n${table}\n`,
    [`loadtest/results/${name}.md`]: table,
    [`loadtest/results/${name}.json`]: JSON.stringify(data, null, 2),
  };
}

function row(label, m, opts) {
  if (!m) return `| ${label} | | | | | | | | no samples |`;
  const v = m.values;
  const thresholds = Object.entries(m.thresholds || {})
    .map(([t, r]) => `${t} ${r.ok ? "passed" : "FAILED"}`)
    .join(", ");
  switch (m.type) {
    case "trend":
      return `| ${label} (ms) | ${ms(v.avg)} | ${ms(v["p(50)"])} | ${ms(v["p(95)"])} | ${ms(v["p(99)"])} | ${ms(v.max)} | | | ${thresholds} |`;
    case "counter": {
      const rate = opts.seconds ? v.count / opts.seconds : v.rate;
      return `| ${label} | | | | | | ${thousands(v.count)} | ${rate.toFixed(1)}/s | ${thresholds} |`;
    }
    case "rate":
      return `| ${label} | | | | | | ${thousands(v.passes)} of ${thousands(v.passes + v.fails)} | ${(v.rate * 100).toFixed(2)}% | ${thresholds} |`;
    default:
      return `| ${label} | | | | | | ${thousands(v.value)} | | ${thresholds} |`;
  }
}

const ms = (x) => (x === undefined ? "" : x.toFixed(1));

export function thousands(n) {
  return String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}
