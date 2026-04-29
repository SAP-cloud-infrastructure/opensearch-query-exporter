FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -ldflags="-w -s" \
    -o opensearch-query-exporter \
    ./cmd/exporter

FROM alpine:3.21

RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 -S exporter && \
    adduser -u 1000 -S exporter -G exporter && \
    mkdir -p /config && \
    chown exporter:exporter /config

COPY --from=builder /build/opensearch-query-exporter /usr/local/bin/opensearch-query-exporter

USER exporter

EXPOSE 9206

ENTRYPOINT ["opensearch-query-exporter"]
CMD ["-config", "/config/config.yaml"]
