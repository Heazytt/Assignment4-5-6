# syntax=docker/dockerfile:1.6
# Generic builder for any of the cmd/<service>/main.go services.
# Pass --build-arg SERVICE=<name> at build time.

FROM golang:1.22-alpine AS build
ARG SERVICE
WORKDIR /src

# GOFLAGS=-mod=mod means Go will automatically create/update go.sum
# without requiring it to exist beforehand.
ENV GOFLAGS="-mod=mod"
ENV GONOSUMDB="*"

# Cache module downloads.
COPY go.mod ./
COPY go.su[m] ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy full source.
COPY . .

# Build the requested service binary.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" \
    -o /out/app ./cmd/${SERVICE}

# ----------------- runtime stage -----------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget && \
    addgroup -S app && adduser -S -G app app

WORKDIR /app
COPY --from=build /out/app /app/app

USER app
EXPOSE 8080 9000

ENTRYPOINT ["/app/app"]
