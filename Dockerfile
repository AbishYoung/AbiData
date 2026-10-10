# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
# go.mod replaces gopher-lua with this vendored fork, which must exist before `go mod download`.
COPY third_party/ ./third_party/
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/abidata ./cmd/abidata
RUN mkdir -p /out/data /out/backups

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/abidata /abidata
COPY --from=build --chown=65532:65532 /out/data /var/lib/abidata
COPY --from=build --chown=65532:65532 /out/backups /var/lib/abidata-backups
ENV ABIDATA_DATA_DIR=/var/lib/abidata \
    ABIDATA_BACKUP_DIR=/var/lib/abidata-backups
USER 65532
EXPOSE 8080
VOLUME ["/var/lib/abidata", "/var/lib/abidata-backups"]
HEALTHCHECK CMD ["/abidata","healthcheck"]
ENTRYPOINT ["/abidata","serve"]
