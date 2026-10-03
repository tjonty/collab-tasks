// Opening a big project: loading every task summary of a seeded 10,000-task
// project page by page, gzipped, the way the web app does when it opens one.
// Ten people at once, for 30 seconds.
//
//   docker compose exec api /seed          (ends with the project's URL)
//   k6 run -e PROJECT_ID=<id> loadtest/initial-load.js
//   k6 run -e SMOKE=1 -e PROJECT_ID=<id> loadtest/initial-load.js
import { check } from "k6";
import http from "k6/http";
import { Trend } from "k6/metrics";

import { API, SMOKE, TREND_STATS, createUser, getProject, thousands, writeSummary } from "./lib.js";

const PROJECT_ID = __ENV.PROJECT_ID;
if (!PROJECT_ID) {
  throw new Error("Pass -e PROJECT_ID=<id> of a seeded project. Seed one with: docker compose exec api /seed");
}

const fullLoadMs = new Trend("full_load_ms", true);

export const options = {
  summaryTrendStats: TREND_STATS,
  scenarios: {
    open_project: SMOKE
      ? { executor: "per-vu-iterations", vus: 1, iterations: 1 }
      : { executor: "constant-vus", vus: 10, duration: "30s" },
  },
  thresholds: {
    full_load_ms: ["p(95)<2000"],
    checks: ["rate==1"],
    http_req_failed: ["rate==0"],
    // These two make k6 report the opens' own pages and bytes, apart from setup's.
    "http_req_duration{scenario:open_project}": ["max>=0"],
    "data_received{scenario:open_project}": ["count>0"],
  },
};

// Every page of the project's tasks: how many tasks, and how many characters
// of JSON after decompressing (the summaries are ASCII, so that's bytes).
function loadAll(userId, gzip) {
  const headers = { "X-User-Id": userId };
  if (gzip) headers["Accept-Encoding"] = "gzip";
  let after = null;
  let count = 0;
  let bytes = 0;
  do {
    const res = http.get(`${API}/projects/${PROJECT_ID}/tasks?limit=500${after ? `&after=${after}` : ""}`, {
      headers,
      tags: { name: "listTasks" },
    });
    if (res.status !== 200) throw new Error(`list tasks: HTTP ${res.status}`);
    const page = res.json();
    count += page.tasks.length;
    bytes += res.body.length;
    after = page.nextCursor;
  } while (after);
  return { count, bytes };
}

export function setup() {
  const user = createUser("loadtest-reader");
  getProject(user.id, PROJECT_ID);
  // Once, uncompressed: how many tasks to expect, and the JSON's full size.
  const plain = loadAll(user.id, false);
  return { userId: user.id, expected: plain.count, uncompressedBytes: plain.bytes };
}

export default function (data) {
  const start = Date.now();
  const { count } = loadAll(data.userId, true);
  fullLoadMs.add(Date.now() - start);
  check(count, { "loaded every task": (n) => n === data.expected });
}

export function handleSummary(data) {
  const opens = data.metrics.iterations ? data.metrics.iterations.values.count : 0;
  const received = data.metrics["data_received{scenario:open_project}"];
  const perOpen = opens && received ? received.values.count / opens : 0;
  const s = data.setup_data || {};
  return writeSummary(
    "initial-load",
    data,
    [
      ["opening the project: every summary", "full_load_ms"],
      ["one page of 500 summaries", "http_req_duration{scenario:open_project}"],
      ["loaded every task", "checks"],
      ["failed requests", "http_req_failed"],
    ],
    [
      `Tasks in the project: ${thousands(s.expected || 0)}. Opens: ${thousands(opens)}.`,
      `JSON for every summary, uncompressed: ${thousands(s.uncompressedBytes || 0)} bytes. Received per open with gzip, headers included: ${thousands(perOpen)} bytes.`,
    ],
  );
}
