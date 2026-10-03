# fanout

Run at 2026-10-03T16:44:34.993Z against http://localhost:8080.
Environment: not recorded (pass -e ENVIRONMENT=…).

| Measure | avg | p50 | p95 | p99 | max | count | rate | Thresholds |
| ------- | --- | --- | --- | --- | --- | ----- | ---- | ---------- |
| a change reaching a listener (ms) | 19.4 | 17.0 | 37.0 | 47.0 | 80.0 | | | p(95)<250 passed, p(99)<500 passed |
| deliveries | | | | | | 90,000 | 1284.3/s |  |
| seq gaps | | | | | | 0 | 0.0/s | count==0 passed |
| listeners caught up | | | | | | 200 of 200 | 100.00% | rate==1 passed |
| writer: a change (ms) | 11.2 | 8.8 | 24.7 | 34.1 | 73.5 | | |  |
| failed requests | | | | | | 0 of 454 | 0.00% | rate<0.01 passed |
