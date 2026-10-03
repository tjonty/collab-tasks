// Write throughput. Every change to a project first takes that project's
// row lock (it's how changes get a gapless seq), so 50 people writing to one
// project apply one at a time, while 50 people spread over 20 projects don't
// wait on each other. This measures both.
//
//   k6 run loadtest/writes.js            (about 2.5 minutes)
//   k6 run -e SMOKE=1 loadtest/writes.js (seconds: checks the script)
import { check } from "k6";

import { SMOKE, TREND_STATS, createProject, createTasks, createUser, submit, writeSummary } from "./lib.js";

const VUS = SMOKE ? 2 : 50;
const SECONDS = SMOKE ? 5 : 60;
const SPREAD_PROJECTS = 20;
const PRIORITIES = ["low", "medium", "high", "urgent"];
// k6 numbers VUs across both scenarios, so ids run up to 2 × VUS.
const MAX_VU_ID = 2 * VUS;

export const options = {
  summaryTrendStats: TREND_STATS,
  scenarios: {
    hot_project: { executor: "constant-vus", exec: "hotProject", vus: VUS, duration: `${SECONDS}s`, startTime: "0s" },
    spread: { executor: "constant-vus", exec: "spread", vus: VUS, duration: `${SECONDS}s`, startTime: `${SECONDS + 10}s` },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    "http_req_duration{scenario:hot_project}": ["p(95)<300"],
    "http_req_duration{scenario:spread}": ["p(95)<150"],
    // These two make k6 report each scenario's iterations.
    "iterations{scenario:hot_project}": ["count>0"],
    "iterations{scenario:spread}": ["count>0"],
  },
};

export function setup() {
  const ts = Date.now();
  const user = createUser("loadtest-writer");
  const hotId = createProject(user.id, `loadtest-hot-${ts}`);
  // Two tasks for every VU id, so no two VUs ever share a task.
  const hot = { projectId: hotId, taskIds: createTasks(user.id, hotId, 2 * MAX_VU_ID) };
  const spread = [];
  for (let p = 0; p < SPREAD_PROJECTS; p++) {
    const id = createProject(user.id, `loadtest-spread-${p}-${ts}`);
    spread.push({ projectId: id, taskIds: createTasks(user.id, id, 20) });
  }
  return { userId: user.id, hot, spread };
}

// Each VU changes only its own tasks, so a status move never meets another
// VU's: a 409 here would be a real error, not load.
export function hotProject(data) {
  const i = __VU - 1;
  change(data.userId, data.hot.projectId, [data.hot.taskIds[2 * i], data.hot.taskIds[2 * i + 1]]);
}

export function spread(data) {
  const i = __VU - 1;
  const project = data.spread[i % SPREAD_PROJECTS];
  change(data.userId, project.projectId, [project.taskIds[Math.floor(i / SPREAD_PROJECTS)]]);
}

// This VU's view of its tasks' statuses, so every move is a valid one.
const statuses = {};

// 70% edits, 20% status moves, 10% comments: roughly what people do.
function change(userId, projectId, taskIds) {
  const taskId = taskIds[__ITER % taskIds.length];
  const r = Math.random();
  let c;
  if (r < 0.7) {
    c = { type: "task.update", taskId, title: `loadtest ${__ITER}`, priority: PRIORITIES[__ITER % PRIORITIES.length] };
  } else if (r < 0.9) {
    c = { type: "task.setStatus", taskId, to: statuses[taskId] === "in_progress" ? "todo" : "in_progress" };
  } else {
    c = { type: "comment.create", id: crypto.randomUUID(), taskId, content: `loadtest comment ${__ITER}` };
  }
  const res = submit(userId, projectId, c);
  check(res, { "status is 200": (res) => res.status === 200 });
  if (c.type === "task.setStatus" && res.status === 200) statuses[taskId] = c.to;
}

export function handleSummary(data) {
  return writeSummary("writes", data, [
    ["one project: a change", "http_req_duration{scenario:hot_project}"],
    ["one project: changes", "iterations{scenario:hot_project}", { seconds: SECONDS }],
    ["20 projects: a change", "http_req_duration{scenario:spread}"],
    ["20 projects: changes", "iterations{scenario:spread}", { seconds: SECONDS }],
    ["failed requests", "http_req_failed"],
  ]);
}
