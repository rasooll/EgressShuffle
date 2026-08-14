FROM debian:bookworm-slim

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install --no-install-recommends -y tor ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY torrc /etc/tor/torrc
RUN tor --verify-config -f /etc/tor/torrc

USER debian-tor
EXPOSE 9050
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 CMD ["/bin/sh", "-c", "kill -0 1"]
ENTRYPOINT ["tor", "-f", "/etc/tor/torrc"]
