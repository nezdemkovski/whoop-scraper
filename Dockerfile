FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/whoop-scraper ./cmd/whoop-scraper

FROM alpine:3.22

RUN addgroup -S -g 1000 app && adduser -S -u 1000 -G app app
COPY --from=builder /out/whoop-scraper /usr/local/bin/whoop-scraper
USER app

ENTRYPOINT ["whoop-scraper"]
