FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /ember ./cmd/ember

FROM scratch
COPY --from=builder /ember /ember
ENTRYPOINT ["/ember"]
