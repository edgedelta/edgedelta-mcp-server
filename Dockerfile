ARG BUILDPLATFORM=linux/amd64
FROM --platform=$BUILDPLATFORM golang:alpine AS builder
ARG TARGETOS
ARG TARGETARCH
ARG BUILD_VERSION=v0.0.0
ARG BUILD_COMMIT=$(git rev-parse HEAD)
ARG BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH

# Required for establishing https calls
RUN apk update && apk add --no-cache ca-certificates && update-ca-certificates
WORKDIR /app
ADD . .
RUN go build -ldflags="-s -w -X 'main.version=${BUILD_VERSION}' -X 'main.commit=${BUILD_COMMIT}' -X 'main.date=${BUILD_DATE}'" -o edgedelta-mcp-server ./cmd/mcp-server/main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/edgedelta-mcp-server /edgedelta-mcp-server
CMD ["/edgedelta-mcp-server", "stdio"]
