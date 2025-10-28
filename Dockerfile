FROM golang:1.23-bullseye AS builder
WORKDIR /src

# Copy shared library first
COPY shared/ ./shared/

# Copy service files
COPY crl/go.mod crl/go.sum ./crl/
WORKDIR /src/crl
RUN go mod download

WORKDIR /src
COPY crl/ ./crl/
WORKDIR /src/crl
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/crl ./cmd/crl

FROM alpine:3.18
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/crl /usr/local/bin/crl
COPY crl/config/ /config/
EXPOSE 8080 9090
ENTRYPOINT ["/usr/local/bin/crl"]
