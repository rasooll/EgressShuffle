# Contributing to EgressShuffle

Thank you for helping improve EgressShuffle. Contributions should preserve the
project's priorities: correctness, maintainability, operational simplicity,
security, observability, and testability.

By participating, you agree to follow the
[Code of Conduct](CODE_OF_CONDUCT.md).

## Project Scope

EgressShuffle is intended for privacy-preserving outbound testing, controlled
integration testing, and authorized networking research. Contributions must not
add features intended to bypass bans, rate limits, CAPTCHAs, anti-abuse systems,
or access controls.

The following remain outside the project scope:

- Browser automation and crawling frameworks
- User-agent rotation and fingerprint spoofing
- CAPTCHA solving or anti-bot evasion
- Forced Tor identity rotation or `ControlPort` orchestration
- Application-level request replay or retry engines
- Persistent request history, databases, or distributed state
- Kubernetes-specific deployment infrastructure

Open a feature request before investing in a substantial change or introducing
a new dependency.

## Development Requirements

- Go 1.24 or newer
- Docker Engine and Docker Compose v2
- Make
- Bash

Build and test the repository with:

```bash
make build
make check
docker compose config
```

`make check` verifies formatting, runs unit and integration tests, runs the race
detector and `go vet`, and builds the application.

## Reporting Bugs

Search existing issues before opening a new report. Include:

- EgressShuffle version or commit
- Operating system and architecture
- Go, Docker, and Docker Compose versions when relevant
- Deployment and configuration details with credentials removed
- Minimal reproduction steps
- Expected and actual behavior
- Relevant sanitized logs and metric names

Never include proxy credentials, authorization headers, cookies, request or
response bodies, private destination URLs, or other sensitive data.

## Security Vulnerabilities

Do not report vulnerabilities in a public issue. Use GitHub's private
vulnerability reporting or Security Advisories for this repository. If private
reporting is unavailable, contact the maintainer through the method documented
in [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

Include the affected version, impact, reproduction details, and any proposed
mitigation. Do not test against systems or networks without explicit
authorization.

## Proposing Features

Feature requests should describe the operational problem, expected behavior,
security and privacy implications, failure modes, and reasonable alternatives.
Explain why the change belongs in EgressShuffle rather than in a client
application or external platform component.

## Contribution Workflow

1. Fork the repository and create a focused branch from `main`.
2. Keep the change small enough to review and test independently.
3. Follow existing package boundaries and coding conventions.
4. Add or update deterministic tests for behavioral changes.
5. Update documentation and examples when operations or configuration change.
6. Run `make check` and `docker compose config` locally.
7. Open a pull request with a clear problem statement and validation summary.

For Docker or Compose changes, also run:

```bash
docker compose build
```

Changes that require public Tor connectivity must keep that validation separate
from normal automated tests.

## Go Engineering Guidelines

- Prefer the Go standard library and justify new dependencies.
- Keep interfaces small and introduce them only at useful boundaries.
- Avoid global mutable state and unnecessary abstraction layers.
- Use contexts and explicit timeouts for network operations.
- Preserve concurrency safety in discovery, health, balancing, and connection
  accounting.
- Wrap errors with operational context without exposing sensitive data.
- Do not panic for recoverable runtime failures.
- Run `gofmt` and keep `go mod tidy` clean.

## Proxy and Security Requirements

Changes to proxy behavior must preserve these invariants:

- Destination hostnames are passed to SOCKS5 without local DNS resolution.
- Only healthy Tor backends are eligible for new connections.
- `Proxy-Authorization` and hop-by-hop headers are never forwarded.
- `CONNECT 200` is sent only after the upstream SOCKS5 connection succeeds.
- Backend retries occur only before application request transmission.
- Active-connection counters are released on every close and error path.
- Long-lived CONNECT tunnels remain compatible with graceful shutdown.
- Logs and metrics do not expose credentials, query strings, client addresses,
  request bodies, or unbounded labels.

Security-sensitive changes should include focused failure-path and race tests.

## Tests

Tests must be deterministic and must not require public Internet or Tor access.
Use local HTTP servers and the fake SOCKS5 test backend for proxy integration
coverage.

At minimum, run:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Bug fixes should include a regression test that fails without the fix whenever
practical.

## Commits and Pull Requests

Prefer concise Conventional Commit messages, for example:

```text
feat(discovery): reconcile new Docker DNS addresses
fix(proxy): preserve CONNECT half-close behavior
test(health): cover recovery threshold transitions
docs: clarify scale-down interruption risk
```

Pull requests should contain one coherent change and explain:

- What problem is being solved
- Why the chosen approach is appropriate
- Security, privacy, compatibility, and operational effects
- Tests and validation actually executed
- Known limitations or follow-up work

Avoid unrelated formatting, refactoring, or generated-file changes in the same
pull request.

## Documentation

Documentation and examples must describe behavior that exists in the current
implementation. Keep Mermaid diagrams, environment variables, Compose commands,
timeouts, retry semantics, and security guidance synchronized with code.

## License

By contributing, you agree that your contributions will be licensed under the
[Apache License 2.0](LICENSE).
