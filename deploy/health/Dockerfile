# health-wisp image: one static Go binary and nothing else (~5 MB). It
# only serves, never calls out, so it needs no CA certificates, shell or
# libc. The binary is cross-compiled by deploy.sh on the dev machine and
# the image built there too, then shipped (docker save | ssh | docker
# load) — nothing is built on the host.
FROM scratch
COPY health-wisp /health-wisp
USER 65534:65534
EXPOSE 8080
# No shell or wget in the image: the binary probes itself.
HEALTHCHECK --interval=60s --timeout=5s --start-period=10s CMD ["/health-wisp", "-probe", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/health-wisp", "-listen", ":8080"]
