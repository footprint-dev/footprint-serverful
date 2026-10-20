FROM golang:1.27.3-alpine3.24 AS build
ARG GOPROXY=https://proxy.golang.org,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go generate ./ent && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/footprint ./cmd/footprint

FROM alpine:3.24
RUN apk add --no-cache ca-certificates && \
    addgroup -S footprint && adduser -S -G footprint footprint
COPY --from=build /out/footprint /usr/local/bin/footprint
USER footprint
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=30s --retries=3 \
    CMD ["/usr/local/bin/footprint", "-config", "/etc/footprint/config.toml", "-healthcheck"]
ENTRYPOINT ["/usr/local/bin/footprint"]
CMD ["-config", "/etc/footprint/config.toml"]
