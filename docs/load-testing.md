# Load Testing Guide

Three ways to run the k6 load tests, from zero infrastructure to a real cloud cluster.

---

## The test

`k6/load_test.js` runs three scenarios against all five algorithm endpoints simultaneously. Each Virtual User (VU) sends a unique `X-Forwarded-For` IP so every simulated client has its own rate limit bucket — no shared quota.

| Scenario | VUs | Sleep | What it shows |
|----------|-----|-------|---------------|
| **Steady** | 5 | 200ms | Below limit — all algorithms allow 100%. Baseline. |
| **Burst** | 0 → 50 in 3s | 20ms | Spike — Token/Leaky Bucket absorb more than window algorithms |
| **Overload** | 20 | 50ms | 2× limit sustained — per-algorithm degradation pattern |

Custom metrics:
- `unexpected_errors` — anything that is not 200 or 429. Should be 0.00%.
- `http_req_duration` — p95 threshold < 200ms.

---

## Phase 1 — Local (no cluster)

**Requirements:** Go 1.22+, k6

```bash
# Start the gateway
go run ./cmd/gateway &

# Run the test
k6 run k6/load_test.js

# Or with Make
make gateway &
make k6
```

**What you get:** Results in the terminal. No Kubernetes, no infrastructure. Good for iterating on the test script.

---

## Phase 2 — Kind (local Kubernetes)

**Requirements:** Docker Desktop running, kind installed

```bash
# Install kind (macOS/Linux)
brew install kind
# Windows
winget install Kubernetes.kind
```

**Full workflow:**

```bash
make kind-up        # create cluster (port 8080 → gateway, 9090 → prometheus, 3000 → grafana)
make kind-load      # build gateway image + load into kind
make kind-deploy    # apply all manifests, wait for rollout
make kind-test      # run k6

# While k6 runs, open Grafana in another tab
open http://localhost:3000   # anonymous admin, Prometheus pre-wired

make kind-down      # destroy cluster when done
```

**What you get:** Real Kubernetes networking. Gateway running as a pod with liveness/readiness probes. Prometheus scraping `/metrics`. Grafana showing per-algorithm counters in real time.

**Switching between Historia A and Historia B:**

Edit `k8s/configmap.yaml` — uncomment the Historia B block before `kind-deploy`:

```yaml
# Historia B — infrastructure scale
WINDOW_LIMIT: "1000"
CAPACITY: "2000"
RATE_PER_SEC: "1000"
```

Or override at runtime without editing:

```bash
kubectl create configmap gateway-config \
  --from-literal=WINDOW_LIMIT=1000 \
  --from-literal=CAPACITY=2000 \
  --from-literal=RATE_PER_SEC=1000 \
  --from-literal=WINDOW_SECS=1 \
  --namespace rate-limiter \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl rollout restart deployment/gateway -n rate-limiter
```

---

## Phase 2b — GitHub Actions + kind (automated)

No local installation needed. The GitHub runner does everything.

1. Go to **Actions** → **Load Test (k6 + kind)**
2. Click **Run workflow**
3. Choose inputs:
   - **Historia**: `A` (limit=10, algorithm differences visible) or `B` (limit=1 000, infra scale)
   - **Replicas**: `1` for correct rate limiting, `3` to demonstrate the distributed problem
4. Watch the Step Summary for results

The workflow:
- Spins up a kind cluster inside the Ubuntu runner
- Builds the gateway image and loads it into the cluster
- Deploys gateway + Prometheus + Grafana
- Waits for `/healthz`
- Runs k6 with the chosen parameters
- Posts the full k6 output + threshold table + `/stats` JSON to the Step Summary
- Destroys the cluster

Results are uploaded as artifacts (`k6-results.json`, `k6-summary.json`, `k6-output.txt`) — retained 30 days.

---

## Phase 3 — Hetzner k3s (real cloud cluster)

A single-node k3s cluster on Hetzner (~€4/month for a cx21: 2 vCPU, 4 GB RAM).

### 1. Provision the cluster

```bash
# Create terraform.tfvars (never committed — in .gitignore)
cat > terraform/terraform.tfvars <<EOF
hcloud_token   = "your-hetzner-api-token"
ssh_public_key = "$(cat ~/.ssh/id_ed25519.pub)"
EOF

make tf-init
make tf-apply
```

Terraform creates the VPS, installs k3s via cloud-init, and outputs:

```
server_ip          = "1.2.3.4"
gateway_url        = "http://1.2.3.4:30080"
grafana_url        = "http://1.2.3.4:30030"
kubeconfig_command = "scp root@1.2.3.4:/root/kubeconfig.yaml ~/.kube/rate-limiter-k3s.yaml"
```

### 2. Connect kubectl

```bash
scp root@1.2.3.4:/root/kubeconfig.yaml ~/.kube/rate-limiter-k3s.yaml
export KUBECONFIG=~/.kube/rate-limiter-k3s.yaml
kubectl get nodes   # should show 1 Ready node
```

### 3. Deploy manually

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/gateway.yaml
kubectl apply -f k8s/monitoring.yaml

# Build and push image to ghcr.io first
docker build --build-arg CMD=gateway -t ghcr.io/aminch18/rate-limiter-labs/gateway:latest .
docker push ghcr.io/aminch18/rate-limiter-labs/gateway:latest

# Update the deployment image
kubectl set image deployment/gateway gateway=ghcr.io/aminch18/rate-limiter-labs/gateway:latest -n rate-limiter
kubectl rollout status deployment/gateway -n rate-limiter
```

### 4. Connect GitHub Actions

Add the kubeconfig as a GitHub secret so the `deploy-cloud.yml` workflow can deploy automatically:

```bash
# macOS
cat ~/.kube/rate-limiter-k3s.yaml | base64 | pbcopy

# Linux
cat ~/.kube/rate-limiter-k3s.yaml | base64 | xclip -selection clipboard
```

Go to **GitHub repo → Settings → Secrets → Actions → New repository secret**:
- Name: `KUBECONFIG_CLOUD`
- Value: paste the base64 string

Then run **Actions → Deploy & Load Test (Cloud)** → Run workflow.

### 5. Tear down when done

```bash
make tf-destroy   # destroys VPS and firewall — billing stops immediately
```

---

## The distributed rate limiting experiment

This is the most important experiment in the repo.

**With 1 replica** (correct behavior):
```bash
# In-cluster: each client respects its limit
make kind-deploy
k6 run -e TARGET_URL=http://localhost:8080 k6/load_test.js
# → ~50% rejected during overload (as designed)
```

**With 3 replicas** (the problem):
```bash
kubectl scale deployment/gateway --replicas=3 -n rate-limiter
k6 run -e TARGET_URL=http://localhost:8080 k6/load_test.js
# → far fewer rejections — each pod has its own counter
# → a client can send 3× the configured limit before being rejected
```

This is exactly what happens in production when you scale horizontally without a shared backend. The fix is Redis-backed distributed rate limiting — each pod reads and writes the same counter. That's the next step beyond this lab.

---

## Reading the results

**`http_req_failed`** will be high (~50-97%) during burst and overload. This is **correct** — k6 counts 429 responses as "failed" by default. The script overrides this with `http.setResponseCallback(http.expectedStatuses(200, 429))` so `http_req_failed` only captures real errors (5xx, timeouts).

**`unexpected_errors`** is the metric that matters for correctness. It should always be 0.00%.

**`p(95) < 200ms`** — on a local kind cluster you'll see p95 under 5ms. The gateway is extremely fast. On Hetzner with real network latency you'll see more realistic numbers.

**`/stats` endpoint** gives the per-algorithm allowed/denied counts after the test:

```bash
curl http://localhost:8080/stats | python3 -m json.tool
```

```json
[
  {"algorithm": "TokenBucket",    "allowed": 12450, "denied": 850,  "tracked_keys": 50},
  {"algorithm": "FixedWindow",    "allowed": 9200,  "denied": 4100, "tracked_keys": 50},
  {"algorithm": "LeakyBucket",    "allowed": 11800, "denied": 1500, "tracked_keys": 50},
  {"algorithm": "SlidingLog",     "allowed": 9200,  "denied": 4100, "tracked_keys": 50},
  {"algorithm": "SlidingCounter", "allowed": 9200,  "denied": 4100, "tracked_keys": 50}
]
```

Token Bucket and Leaky Bucket will show higher `allowed` counts than the window-based algorithms — that is the burst absorption difference in numbers.
