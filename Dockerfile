# GoReleaser builds the image from the binaries it has just made, so the image
# holds the same bield as the release archives. It puts each one in a directory
# named for its platform, such as linux/arm64/bield. To build it without a
# release, run `goreleaser release --snapshot --clean`.

# distroless/static is CA certificates, time zone data and a nonroot user (65532),
# with no shell or package manager. Dependabot keeps the digest up to date.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/bield /usr/local/bin/bield

# The data directory. WORKDIR makes it owned by the user below, and a new named
# volume copies that owner. A bind mount keeps its own, so it has to be owned
# by 65532.
USER 65532:65532
WORKDIR /data
ENV BIELD_DATA_DIR=/data
VOLUME /data

# Bield only serves the app on the tailnet, so there's nothing to check from
# outside. In embedded mode, it answers on the setup page's loopback address
# whenever it runs, which is what healthcheck asks.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/bield", "healthcheck"]

ENTRYPOINT ["/usr/local/bin/bield"]
CMD ["serve"]
