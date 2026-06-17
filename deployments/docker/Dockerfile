# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
# mattn/go-sqlite3 (the learning store) is cgo-based, so build tooling is required.
# docker buildx builds this stage natively per --platform target, so no
# explicit GOARCH cross-compilation is needed here.
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -ldflags "-s -w -X main.buildVersion=${VERSION} -X github.com/teochenglim/sora/internal/webhook.Version=${VERSION}" \
    -o /out/sora ./cmd/sora

FROM alpine:3.19
RUN apk add --no-cache ca-certificates && \
    addgroup -S sora && adduser -S sora -G sora
WORKDIR /etc/sora

COPY --from=build /out/sora /sora
COPY configs/ /etc/sora/configs/

USER sora
EXPOSE 8080
ENTRYPOINT ["/sora"]
CMD ["--mode=full", "--config=/etc/sora/configs/config.yaml"]
