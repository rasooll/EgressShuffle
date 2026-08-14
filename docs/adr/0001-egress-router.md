# ADR 0001: Native Go Egress Router over a Tor SOCKS5 Pool

## Status

Accepted

## Context

Applications need one conventional HTTP proxy endpoint backed by a dynamic,
health-aware set of independent Tor clients. The router must support ordinary
HTTP, HTTPS `CONNECT`, Docker DNS reconciliation, conservative retry,
destination DNS privacy, metrics, authentication, and graceful tunnel
shutdown.

A common alternative is a chain such as HAProxy to Privoxy to Tor. That adds
multiple configuration languages and process boundaries. Generic TCP balancing
cannot preserve the required per-connection SOCKS5 destination handshake, HTTP
header rules, retry boundary, backend active counts, and integrated readiness
semantics without additional components.

Running multiple Tor processes in one container was also considered. It would
require per-process ports, data directories, supervision, bootstrap state,
health mapping, and signal handling inside one container.

## Decision

Implement a small native Go HTTP forward proxy that selects a healthy backend
and performs SOCKS5 directly. Use Go's standard library for HTTP, networking,
structured logging, metrics exposition, lifecycle, and tests.

Run exactly one Tor process per Tor container. Discover replicas through Docker
DNS by the scalable service name. Treat backend addresses as disposable runtime
identities and retain previous membership when discovery itself fails.

## Alternatives

### HAProxy -> Privoxy -> Tor

Rejected because it increases deployment and operational complexity, makes
dynamic membership and end-to-end health state less cohesive, and obscures the
safe retry point between backend selection and application transmission.
HAProxy is strong at generic load balancing, but this system needs awareness of
HTTP proxy semantics and SOCKS5 destination negotiation in one lifecycle.

### Multiple Tor Processes per Container

Rejected because it recreates a process supervisor and scheduler inside the
container. One process per container gives Docker direct ownership of restart,
resource accounting, logs, health, signals, and horizontal scaling. Disposable
replicas also avoid shared Tor data and ambiguous failure domains.

### Fixed Named Tor Services

Rejected because `tor1`, `tor2`, and similar definitions make scale a static
configuration operation and require router changes or restart. A single
scalable service allows `docker compose up -d --scale tor=N` and periodic DNS
reconciliation.

## Consequences

- The router contains security-sensitive HTTP, SOCKS5, and tunnel lifecycle code that requires focused tests and review.
- The deployment has only two required image types and no Privoxy layer.
- Destination hostnames remain inside SOCKS5 instead of being locally resolved.
- Retry semantics can be enforced before an HTTP transport receives a connection.
- Backend and readiness metrics share the same state used for selection.
- Tor replicas can be scaled and replaced independently without router restart.
- The project owns Prometheus exposition rather than using a client dependency.
- One Tor container per process costs more container metadata but substantially simplifies operations and isolation.
