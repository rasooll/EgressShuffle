# Operations

## Startup

Start the default three-replica stack:

```bash
docker compose up -d --build --scale tor=3
docker compose ps
```

`/healthz` becomes available when the router starts. `/readyz` remains `503`
until Docker DNS discovery has succeeded and at least one backend has crossed
the configured success threshold. Tor bootstrap, descriptor download, and
health convergence commonly make readiness slower than container startup.

```bash
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/readyz
```

## Graceful Shutdown

```bash
docker compose down
```

`SIGTERM` stops discovery and health workers, stops accepting requests, waits
for ordinary requests and hijacked tunnels, and force-closes remaining
connections at `SHUTDOWN_TIMEOUT`. Compose's router `stop_grace_period` is
slightly longer than the default application deadline.

## Scaling Tor

```bash
docker compose up -d --scale tor=5
docker compose up -d --scale tor=10
docker compose up -d --scale tor=2
```

Do not restart EgressShuffle during scaling. Backend membership converges after
approximately one `DISCOVERY_INTERVAL`. New entries require subsequent health
successes; removed entries disappear at reconciliation. A temporary DNS error
retains previous membership until a later successful lookup.

Observe convergence with:

```bash
curl http://127.0.0.1:9090/metrics
docker compose logs egressshuffle
```

Relevant gauges are `egressshuffle_backend_count` and
`egressshuffle_healthy_backend_count`.

## Logs and Metrics

```bash
docker compose logs egressshuffle
docker compose logs tor
curl http://127.0.0.1:9090/metrics
curl http://127.0.0.1:9090/version
```

The minimal router image intentionally has no shell, `curl`, `dig`, `ping`, or
package manager. Run diagnostics from the host. The one useful executable
inspection command inside the image is:

```bash
docker compose exec egressshuffle /egressshuffle --version
```

## Configuration Changes

Environment changes require recreating the router:

```bash
docker compose up -d --force-recreate egressshuffle
```

Validate edits first:

```bash
docker compose config
```

Authentication credentials should be supplied through a protected deployment
environment rather than committed files. Docker Compose environment variables
are not a secret store and can be visible to users with Docker access.

## Upgrades

1. Review application, Go, Debian, Tor, distroless, Prometheus, and workflow action updates.
2. Run `make check` and `docker compose build`.
3. Validate `docker compose config`.
4. Rebuild and recreate the router and Tor replicas during an approved window.
5. Confirm liveness, readiness, backend counts, logs, and proxy traffic.

Tor state is disposable and stored in tmpfs. Recreating all Tor replicas causes
fresh bootstrap and temporarily removes readiness. A rolling replica replacement
reduces interruption when uninterrupted service is required.

## Capacity

Capacity depends on traffic shape, tunnel duration, host file descriptors, CPU,
memory, network latency, Tor circuit behavior, and destination behavior. More
replicas do not provide linear performance or unique exits.

| Approximate pool | Operational consideration |
| --- | --- |
| 1 Tor | Lowest resource use and no backend redundancy |
| 5 Tor | Useful small pool; modest memory, sockets, bootstrap traffic, and health load |
| 10 Tor | More path diversity and failure tolerance, with higher host and Tor network cost |
| 50 Tor | Significant memory, descriptors, DNS answers, health concurrency, bootstrap load, and diminishing returns |

Each active proxied connection consumes client and upstream file descriptors;
CONNECT tunnels may be long-lived. Review host and Docker descriptor limits,
memory pressure, and metric trends before increasing concurrency. Compose gives
each Tor replica a `nofile` limit of 8192; host limits remain authoritative.

## Optional Prometheus

```bash
docker compose --profile observability up -d --build --scale tor=3
```

Prometheus is available at `http://127.0.0.1:9091`. Its storage is tmpfs and is
not persistent by design. Production operators should provide their own
retention, authentication, and access controls.

## Smoke Test

With the stack running and ready:

```bash
make smoke-test
```

This checks liveness, readiness, metrics, HTTP, and HTTPS. It intentionally
requires public Tor connectivity and is separate from deterministic CI tests.
