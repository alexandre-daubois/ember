FROM golang:1.26-alpine AS builder

ARG VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=${VERSION}" -o /ember ./cmd/ember

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /ember /ember

ENTRYPOINT ["/ember"]
CMD ["--daemon", "--expose", ":9191"]
