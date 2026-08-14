# Security

## Security Posture

EgressShuffle is an outbound proxy, not an anonymity product or access-control
gateway. Its defaults reduce accidental exposure, but operators remain
responsible for host security, client behavior, destination policy, and legal
authorization.

## Network Exposure

Compose publishes proxy and admin listeners on `127.0.0.1`. Changing
`PROXY_BIND` or `ADMIN_BIND` to `0.0.0.0` makes them reachable on host network
interfaces and can create an open proxy or expose metrics and build metadata.
Use a firewall, a trusted private network, and authentication before broadening
either binding.

The application defaults also bind to loopback when run outside a container.
Compose overrides container listeners to `0.0.0.0` because Docker port
forwarding must reach them; host-side bindings remain loopback-only.

Tor SOCKS ports are never published. `tor_internal` is an internal Docker
network shared by the router and Tor. Each Tor container also needs
`tor_egress` to reach the Tor network. The router's `client_ingress` attachment
is required for host port publication and gives the container ordinary network
reachability at the Docker layer; application destination dials are nonetheless
implemented exclusively through SOCKS5.

The internal network uses a dedicated subnet, and Tor's `SocksPolicy` accepts
only that subnet. SOCKS requests arriving through Tor's separate egress
interface are rejected.

## Authentication and Plaintext Risk

Optional HTTP Basic proxy authentication uses constant-time comparison of
fixed-size credential hashes. `Proxy-Authorization` is consumed and always
removed before forwarding. Credentials are never logged.

Basic authentication is not encryption. A client-to-proxy connection over an
untrusted network exposes credentials and plain HTTP traffic. When remote
access is necessary, use an authenticated TLS tunnel, VPN, or equivalent
trusted encrypted transport in front of EgressShuffle. HTTPS protects the
client-to-destination payload after `CONNECT`, but CONNECT metadata and proxy
credentials still need client-to-proxy protection.

## DNS Leak Considerations

Destination hostnames are sent as SOCKS5 domain-name values. EgressShuffle does
not resolve them through host or container DNS. Docker DNS resolves only the
configured Tor service name. This prevents a common destination DNS leak in the
router.

This property does not control DNS or other network activity performed directly
by the client application outside the proxy. Literal destination IPs remain
literal. A compromised process or future code that bypasses the SOCKS5 dialer
could use the router's ordinary network path, so tests and review must preserve
the dial boundary.

## Tor and Destination Limitations

- Multiple Tor clients can independently choose the same exit relay.
- Tor decides circuit construction and lifetime; there is no `ControlPort`.
- A SOCKS5 greeting can succeed before Tor has a usable exit circuit.
- Tor exit relays can observe plaintext destination traffic.
- Destinations can block Tor exits or behave differently for Tor users.
- Tor does not remove application identifiers, cookies, account identifiers,
  payload identifiers, TLS fingerprints, or behavioral correlation.

Use end-to-end TLS for sensitive destination traffic. Do not claim guaranteed
IP uniqueness, circuit rotation, or anonymity.

## Header and Logging Privacy

The proxy removes standard hop-by-hop headers, connection-nominated headers,
`Proxy-Connection`, and `Proxy-Authorization`. It does not inspect or sanitize
end-to-end application identifiers.

Routine logs avoid URLs, query strings, request and response bodies, cookies,
authorization values, and client addresses. Request IDs are accepted only when
printable and at most 64 bytes; otherwise a random ID is generated. Request IDs
are log correlation fields, never metric labels.

Metrics expose only bounded operation labels and opaque backend IDs. They do
not expose backend addresses, destinations, query strings, client IPs, request
IDs, user agents, or credentials.

## Container Hardening

The router uses a distroless image, static Go binary, non-root user, read-only
root filesystem, dropped capabilities, and `no-new-privileges`. It needs no
writable filesystem.

Tor runs as Debian's `debian-tor` user with dropped capabilities and a read-only
root filesystem. `/tmp` is an in-memory writable filesystem because Tor needs a
data directory for consensus and circuit state. It is mounted with `noexec`,
`nosuid`, and `nodev`. Data is intentionally discarded with the container.

Docker isolation does not protect against a compromised Docker daemon or
kernel, and image tags and upstream base images still require normal supply
chain maintenance.

## Authorized Use

Operate EgressShuffle only against systems and networks for which you have
explicit authorization. It must not be deployed to evade bans, rate limits,
CAPTCHAs, anti-abuse controls, or access restrictions.
