# writes

Run at 2026-10-03T16:34:51.992Z against http://localhost:8080.
Environment: Apple M1 Pro (8 cores, 16 GB), Docker Desktop 4.12 VM (4 CPUs, 7.7 GB), one API instance.

| Measure | avg | p50 | p95 | p99 | max | count | rate | Thresholds |
| ------- | --- | --- | --- | --- | --- | ----- | ---- | ---------- |
| one project: a change (ms) | 103.5 | 98.0 | 135.2 | 200.4 | 450.9 | | | p(95)<300 passed |
| one project: changes | | | | | | 28,963 | 482.7/s | count>0 passed |
| 20 projects: a change (ms) | 55.5 | 52.5 | 73.0 | 114.2 | 454.3 | | | p(95)<150 passed |
| 20 projects: changes | | | | | | 53,949 | 899.1/s | count>0 passed |
| failed requests | | | | | | 0 of 83,534 | 0.00% | rate<0.01 passed |
