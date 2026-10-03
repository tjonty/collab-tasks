# initial-load

Run at 2026-10-03T16:47:36.473Z against http://localhost:8080.
Environment: Apple M1 Pro (8 cores, 16 GB), Docker Desktop 4.12 VM (4 CPUs, 7.7 GB), one API instance.

| Measure | avg | p50 | p95 | p99 | max | count | rate | Thresholds |
| ------- | --- | --- | --- | --- | --- | ----- | ---- | ---------- |
| opening the project: every summary (ms) | 417.1 | 411.0 | 502.0 | 583.8 | 673.0 | | | p(95)<2000 passed |
| one page of 500 summaries (ms) | 19.2 | 18.2 | 30.7 | 39.9 | 97.2 | | | max>=0 passed |
| loaded every task | | | | | | 723 of 723 | 100.00% | rate==1 passed |
| failed requests | | | | | | 0 of 14,482 | 0.00% | rate==0 passed |

Tasks in the project: 10,000. Opens: 723.

JSON for every summary, uncompressed: 2,586,735 bytes. Received per open with gzip, headers included: 393,233 bytes.
