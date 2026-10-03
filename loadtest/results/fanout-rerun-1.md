# fanout

Run at 2026-10-03T16:39:50.625Z against http://localhost:8080.
Environment: not recorded (pass -e ENVIRONMENT=…).

| Measure | avg | p50 | p95 | p99 | max | count | rate | Thresholds |
| ------- | --- | --- | --- | --- | --- | ----- | ---- | ---------- |
| a change reaching a listener (ms) | 19.4 | 18.0 | 36.0 | 45.0 | 77.0 | | | p(95)<250 passed, p(99)<500 passed |
| deliveries | | | | | | 90,000 | 1284.6/s |  |
| seq gaps | | | | | | 0 | 0.0/s | count==0 passed |
| listeners caught up | | | | | | 200 of 200 | 100.00% | rate==1 passed |
| writer: a change (ms) | 11.7 | 9.8 | 24.7 | 32.8 | 60.9 | | |  |
| failed requests | | | | | | 0 of 454 | 0.00% | rate<0.01 passed |
