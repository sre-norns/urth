# syntax=docker/dockerfile:1
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM golang:1.27.1 AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/
ARG TARGETOS=linux
ARG TARGETARCH
RUN test "$TARGETOS" = linux && \
    case "$TARGETARCH" in amd64|arm64) ;; *) exit 1 ;; esac
ENV GOOS=$TARGETOS GOARCH=$TARGETARCH
# Writable state is explicit; the final runtime has no shell or package manager.
RUN mkdir -p /runtime/home/urth/.config/urth /runtime/home/urth/worker /runtime/tmp /runtime/etc && \
    printf 'urth:x:65532:65532:Urth:/home/urth:/sbin/nologin\n' > /runtime/etc/passwd && \
    printf 'urth:x:65532:\n' > /runtime/etc/group && \
    chown -R 65532:65532 /runtime/home/urth && chmod 1777 /runtime/tmp

FROM build AS api-build
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/urth ./cmd/api-server

FROM build AS worker-build
RUN go build -tags=urth_native -trimpath -buildvcs=false -ldflags='-s -w' -o /out/urth ./cmd/nats-worker

FROM build AS cli-build
RUN go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/urth ./cmd/urthctl

FROM scratch AS runtime
ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED
LABEL org.opencontainers.image.source="https://github.com/sre-norns/urth" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.created=$CREATED
COPY --from=build /runtime/ /
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV HOME=/home/urth XDG_CONFIG_HOME=/home/urth/.config SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
WORKDIR /home/urth
USER 65532:65532

FROM runtime AS api-server
ENV GIN_MODE=release
LABEL org.opencontainers.image.title="Urth API server"
COPY --from=api-build /out/urth /usr/local/bin/urth-api-srv
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/urth-api-srv"]

FROM runtime AS worker
LABEL org.opencontainers.image.title="Urth Worker" \
      org.sre-norns.urth.runtime-profile="native"
COPY --from=worker-build /out/urth /usr/local/bin/urth-worker
ENTRYPOINT ["/usr/local/bin/urth-worker"]

FROM runtime AS cli
LABEL org.opencontainers.image.title="Urth CLI"
COPY --from=cli-build /out/urth /usr/local/bin/urthctl
ENTRYPOINT ["/usr/local/bin/urthctl"]
