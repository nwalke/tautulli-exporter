FROM scratch
WORKDIR /go/bin
COPY .build/tautulli_exporter-linux-amd64 /go/bin/tautulli_exporter
EXPOSE 9487/tcp
ENTRYPOINT ["./tautulli_exporter"]
