# fanout

Run at 2026-10-03T16:38:12.871Z against http://localhost:8080.
Environment: Apple M1 Pro (8 cores, 16 GB), Docker Desktop 4.12 VM (4 CPUs, 7.7 GB), one API instance.

| Measure | avg | p50 | p95 | p99 | max | count | rate | Thresholds |
| ------- | --- | --- | --- | --- | --- | ----- | ---- | ---------- |
| a change reaching a listener (ms) | 39.7 | 23.0 | 89.0 | 669.0 | 942.0 | | | p(95)<250 passed, p(99)<500 FAILED |
| deliveries | | | | | | 89,400 | 1275.0/s |  |
| seq gaps | | | | | | 0 | 0.0/s | count==0 passed |
| listeners caught up | | | | | | 200 of 200 | 100.00% | rate==1 passed |
| writer: a change (ms) | 27.7 | 13.2 | 64.6 | 412.4 | 736.5 | | |  |
| failed requests | | | | | | 0 of 451 | 0.00% | rate<0.01 passed |
