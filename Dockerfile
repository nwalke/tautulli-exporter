FROM scratch

# TARGETOS is an automatic platform ARG enabled by Docker BuildKit.
ARG TARGETOS
# TARGETARCH is an automatic platform ARG enabled by Docker BuildKit.
ARG TARGETARCH

WORKDIR /go/bin
COPY .build/tautulli_exporter-$TARGETOS-$TARGETARCH /go/bin/tautulli_exporter
EXPOSE 9487/tcp
ENTRYPOINT ["./tautulli_exporter"]
