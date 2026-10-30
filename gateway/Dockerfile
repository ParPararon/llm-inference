
FROM golang:1.25.4-trixie AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build \
    -mod=readonly \
    -trimpath \
    -ldflags="-s -w" \
    -o /gateway \
    ./cmd/gateway

FROM scratch

COPY --from=builder /gateway /gateway

USER 10001:10001

EXPOSE 8080

ENTRYPOINT ["/gateway"]
