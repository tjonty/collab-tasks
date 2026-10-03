// Fan-out: 200 people watching one project over WebSockets while one person
// makes 10 changes a second. Measures how long each change takes to reach
// every listener, and that no listener misses an event or gets one out of
// order.
//
//   k6 run loadtest/fanout.js            (about 1.5 minutes)
//   k6 run -e SMOKE=1 loadtest/fanout.js (20 seconds: checks the script)
//
// It holds 200 WebSockets open. macOS shells default to 256 open files, so
// run `ulimit -n 4096` first if k6 reports "too many open files".
import { check } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";
import { WebSocket } from "k6/websockets";

import { SMOKE, TREND_STATS, WS, createProject, createTasks, createUser, getProject, submit, writeSummary } from "./lib.js";

const LISTENERS = SMOKE ? 5 : 200;
// Listeners stay subscribed until the writer is done, plus a margin.
const LISTEN_MS = SMOKE ? 18_000 : 70_000;

const deliveryMs = new Trend("delivery_ms", true);
const deliveries = new Counter("deliveries");
const seqGaps = new Counter("seq_gaps");
const caughtUp = new Rate("caught_up");

export const options = {
  summaryTrendStats: TREND_STATS,
  scenarios: {
    // One subscription per listener: a second one would replay old events,
    // whose timestamps would read as huge delivery times.
    listeners: { executor: "per-vu-iterations", exec: "listen", vus: LISTENERS, iterations: 1, maxDuration: `${LISTEN_MS / 1000 + 20}s` },
    // Starts once every listener has subscribed and caught up.
    writer: SMOKE
      ? { executor: "constant-arrival-rate", exec: "write", rate: 2, timeUnit: "1s", duration: "10s", startTime: "5s", preAllocatedVUs: 2 }
      : { executor: "constant-arrival-rate", exec: "write", rate: 10, timeUnit: "1s", duration: "45s", startTime: "20s", preAllocatedVUs: 5 },
  },
  thresholds: {
    delivery_ms: ["p(95)<250", "p(99)<500"],
    seq_gaps: ["count==0"],
    caught_up: ["rate==1"],
    http_req_failed: ["rate<0.01"],
  },
};

export function setup() {
  const user = createUser("loadtest-fanout");
  const projectId = createProject(user.id, `loadtest-fanout-${Date.now()}`);
  const [taskId] = createTasks(user.id, projectId, 1);
  return { userId: user.id, projectId, taskId, startSeq: getProject(user.id, projectId).seq };
}

export function listen(data) {
  const ws = new WebSocket(`${WS}/ws?userId=${data.userId}`);
  // Every event must follow the one before, starting right after setup.
  let prev = data.startSeq;
  let caught = false;
  seqGaps.add(0);

  ws.onopen = () => ws.send(JSON.stringify({ type: "subscribe", projectId: data.projectId, fromSeq: data.startSeq }));
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.type === "caughtUp") {
      caught = true;
      caughtUp.add(true);
    } else if (msg.type === "event") {
      const { seq, change } = msg.event;
      if (seq !== prev + 1) seqGaps.add(1);
      prev = seq;
      // Only the writer's changes count: their title is when they were sent.
      const sent = /^t:(\d+)$/.exec(change.title || "");
      if (sent) {
        deliveryMs.add(Date.now() - Number(sent[1]));
        deliveries.add(1);
      }
    }
  };
  ws.onclose = () => {
    if (!caught) caughtUp.add(false);
  };
  setTimeout(() => ws.close(), LISTEN_MS);
}

export function write(data) {
  const res = submit(data.userId, data.projectId, { type: "task.update", taskId: data.taskId, title: `t:${Date.now()}` });
  check(res, { "status is 200": (r) => r.status === 200 });
}

export function handleSummary(data) {
  return writeSummary("fanout", data, [
    ["a change reaching a listener", "delivery_ms"],
    ["deliveries", "deliveries"],
    ["seq gaps", "seq_gaps"],
    ["listeners caught up", "caught_up"],
    ["writer: a change", "http_req_duration"],
    ["failed requests", "http_req_failed"],
  ]);
}
