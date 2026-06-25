# Generic multi-stage build for any service in this monorepo.
# Select which one with: --build-arg SERVICE=<gateway|user|wallet|notification>
# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src

# Cache dependencies separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG SERVICE
# Static, stripped binary so it runs on a distroless/scratch base.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}

# Writable data dir owned by the non-root runtime user (65532). A named volume
# mounted here inherits this ownership, so the wallet service can write its
# keystore. Harmless for the stateless services.
RUN mkdir -p /data/keystore

# Distroless, non-root, no shell or package manager — minimal attack surface.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
COPY --from=build --chown=65532:65532 /data /data
USER nonroot:nonroot
ENTRYPOINT ["/app"]
