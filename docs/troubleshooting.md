# Troubleshooting

## First Checks

```bash
docker compose ps
docker compose logs egressshuffle
docker compose logs tor
curl http://127.0.0.1:9090/healthz
curl http://127.0.0.1:9090/readyz
curl http://127.0.0.1:9090/metrics
```

Do not assume the router container contains diagnostic utilities. It is
distroless. Run network commands from the host; inside-container version output
is available with:

```bash
docker compose exec egressshuffle /egressshuffle --version
```

## No Healthy Tor Backends

Symptoms: `/readyz` returns `503`, healthy backend gauge is zero, and proxy
requests return `502` or `503`.

- Confirm `egressshuffle_backend_count` is nonzero.
- Inspect Tor logs for bootstrap progress or process failures.
- Wait for `BACKEND_SUCCESS_THRESHOLD` checks after bootstrap.
- Confirm `TOR_SERVICE_NAME=tor` and port `9050` match Compose.
- Check discovery and health failure counters.

## Tor Bootstrap Delayed

Tor can take time to download consensus and descriptors and build circuits.
`docker compose ps` health only confirms that the Tor process is alive; the
router's base check confirms SOCKS5 greeting, not a complete exit circuit.

Inspect:

```bash
docker compose logs tor
```

Look for `Bootstrapped 100% (done)`. Persistent delay can indicate host clock,
firewall, network censorship, DNS, or upstream connectivity problems.

## SOCKS5 Connection Refused

- Confirm all Tor replicas are running.
- Confirm no custom network or port override changed `9050`.
- Check `egressshuffle_backend_failures_total` and health failures.
- Rebuild if `deploy/tor/torrc` changed.
- Do not publish the SOCKS port as a workaround; fix internal connectivity.

## CONNECT Timeout

A `502` during CONNECT can mean all backend handshakes failed, Tor had no usable
circuit, the destination was unreachable, or `CONNECT_TIMEOUT` was too short.
Inspect backend failure counters and Tor logs. Increase the timeout only after
confirming expected network latency; a larger timeout also delays failure.

## Destination Blocks Tor

Some destinations reject Tor exits. If health is ready and other destinations
work, this is destination policy rather than router health. EgressShuffle does
not evade blocks, CAPTCHAs, or anti-abuse systems.

## Docker DNS Lookup Failures

Symptoms include increasing `egressshuffle_discovery_errors_total`. A failed
lookup intentionally retains previous membership. Check Docker network state
and service status:

```bash
docker compose ps
docker compose logs egressshuffle
docker network inspect egressshuffle_tor_internal
```

Successful future discovery reconciles the set automatically.

## readyz Returns 503

Readiness requires both initial successful discovery and one healthy backend.
Compare backend and healthy-backend gauges. Immediately after startup or scale
up, allow roughly discovery plus success-threshold convergence time.

## Backend Repeatedly Flapping

- Inspect Tor bootstrap and connectivity logs.
- Check host CPU, memory, and file descriptor pressure.
- Verify health timeout is realistic for the environment.
- Increase success/failure thresholds cautiously rather than masking failures.
- Consider an operator-controlled end-to-end health URL for stronger checks.

## High Latency

Tor latency is inherently variable. Compare ordinary HTTP and CONNECT traffic,
backend failures, active connections, pool size, and destination behavior.
`least_connections` can reduce concentration of long-lived tunnels but cannot
remove Tor path latency. More replicas do not guarantee lower latency.

## Proxy Authentication Failure

A missing or invalid credential returns `407 Proxy Authentication Required`.
Confirm all three authentication variables are set and the router was
recreated. For curl:

```bash
curl --proxy http://127.0.0.1:8080 \
  --proxy-user 'proxy-user:proxy-password' \
  https://example.com
```

Do not put credentials in URLs, logs, shell history, or committed `.env` files
in sensitive environments.

## Host Ports Are Unavailable

Confirm Compose shows explicit loopback mappings rather than only exposed
container ports:

```bash
docker compose ps
docker compose config
```

Expected mappings are `127.0.0.1:8080->8080/tcp` and
`127.0.0.1:9090->9090/tcp`. Check for host port conflicts and ensure the router
is attached to `client_ingress`; host publication may not work when a container
is attached only to an internal Docker network.
