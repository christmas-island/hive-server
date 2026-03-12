FROM golang:1.25-alpine AS builder
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
  -o /hive-server ./cmd/hive-server

# Intermediate stage: extract only the cert bundle and timezone data.
FROM alpine:3.21 AS certs
RUN apk add --no-cache ca-certificates tzdata

# Final stage: scratch — no shell, no package manager, minimal attack surface.
FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=certs /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /hive-server /usr/bin/hive-server
ENTRYPOINT ["/usr/bin/hive-server"]
CMD ["serve"]
