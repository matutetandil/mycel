# Mycel Benchmark

Measures Mycel on the cheapest hardware anyone would deploy it on: $5/month
Linode VPS, 1 vCPU, 1 GB RAM, with PostgreSQL on a separate machine over the
public network.

Results of the last full run are in [RESULTS.md](RESULTS.md).

## What it runs

Five machines, three tests, in parallel — each test against its own Mycel
instance, so they cannot contend with each other:

```
                         ┌── target-standard  ─── loadtest.js
attacker (4 vCPU) ───────┼── target-realistic ─── realistictest.js
                         └── target-stress    ─── stresstest.js
                                    │
                               database (PG)
```

| Test | What it measures | Endpoints |
|------|------------------|-----------|
| **Standard** | Mycel itself: HTTP parse, CEL evaluation, JSON serialization. No external I/O. | `/ping`, `/echo`, `/process` |
| **Realistic** | What a deployed service does: receive, transform, read and write a database across the network. | `/users` (POST, GET) |
| **Stress** | Deliberately past the calibrated limit: 12 transforms per request, 100-item arrays, 100 KB bodies, 3× the safe VU count, then a recovery check. | all of the above |

### Calibration comes first

Rather than guessing a load, the suite discovers what the hardware sustains: it
steps 10 → 300 VUs, 15 seconds each, and takes the highest level that keeps
errors under 1% and p95 under 500 ms. That number scales the realistic and
stress phases, so the same suite is meaningful on a Nanode and on a 4 vCPU box.

### Preflight comes before that

Every k6 check in this suite is a status check, which makes the load test blind
to a target that is up and wrong. `scripts/preflight.sh` runs first and asserts
the *bodies*: that the transforms ran, that the aggregates are right, that a
written row can be read back. `run.sh` refuses to measure a target that fails
it.

```bash
./scripts/preflight.sh http://<target>:3000        # standard target
./scripts/preflight.sh http://<target>:3000 --db   # target with the CRUD flows
```

It is worth running on its own against any Mycel you are about to load-test.

## Prerequisites

```bash
brew install opentofu    # or https://opentofu.org/docs/intro/install/
brew install k6          # only for `local` mode; `docker` mode needs neither
```

## Configure

```bash
cp terraform.tfvars.example terraform.tfvars
# put your Linode API token in it
```

Pin the version you want to measure — the default is `:latest`, which makes a
result impossible to attribute to a build:

```hcl
mycel_image = "ghcr.io/matutetandil/mycel:3.0.0"
```

## Run

```bash
./run.sh cloud --full        # deploy 5 VPS, run all 3 tests, collect, destroy
./run.sh cloud-local --full  # same, but k6 runs on your machine (4 VPS)

./run.sh local  <ip>         # k6 on your machine, against a target you already have
./run.sh docker <ip>         # k6 in Docker, same
./run.sh remote <ip>         # k6 on the attacker VPS
```

Add `--realistic`, `--stress` or `--full` to pick the test; the default is
standard only.

A 30-second smoke run, to check the harness itself rather than the hardware:

```bash
k6 run --env TARGET_URL=http://localhost:3000 --env SMOKE=true loadtest.js
```

## Results

Written to `results/<timestamp>/`:

| File | What it holds |
|------|---------------|
| `summary.json` | k6 metrics: RPS, latency percentiles, error rate |
| `report.txt` | the formatted report for that test |
| `provenance.txt` | which image and which Mycel version produced these numbers |
| `flow-metrics-*.txt` | Mycel's own flow counters — a second opinion on what k6 sent |
| `resources.csv` | CPU and RAM on the target, sampled every 2s |
| `pre-test.txt` / `post-test.txt` | system state before and after, including OOM kills and restarts |

## Cost

Nanodes are ~$0.0075/hr each and the attacker is ~$0.036/hr, so a full session
runs to a few cents. `run.sh cloud` destroys everything on exit, including when
it fails — but `tofu destroy` is worth running if you ever interrupt it.
