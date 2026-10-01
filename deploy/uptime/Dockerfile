# uptime-wisp image: alpine + CA certificates + one static Go binary.
# The binary is cross-compiled by deploy.sh on the dev machine and
# copied in, so the host needs no Go toolchain, only this tiny build.
FROM alpine:3
RUN apk add --no-cache ca-certificates && adduser -D -H -s /sbin/nologin uptime
COPY uptime-wisp /usr/local/bin/uptime-wisp
USER uptime
EXPOSE 8080
HEALTHCHECK --interval=60s --timeout=5s --start-period=30s CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/uptime-wisp", "-config", "/etc/uptime-wisp/config.json", "-listen", ":8080"]
