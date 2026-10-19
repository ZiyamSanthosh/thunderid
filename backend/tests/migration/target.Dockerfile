# Copyright 2026 The ThunderID Authors
# SPDX-License-Identifier: Apache-2.0

FROM golang:1.27-alpine3.23 AS builder

WORKDIR /src/backend
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 go build -o /out/thunderid ./cmd/server

FROM alpine:3.23

RUN apk add --no-cache ca-certificates

WORKDIR /opt/thunderid
COPY --from=builder /out/thunderid ./thunderid
COPY cmd/server/config ./config
COPY tests/migration/resources ./config/resources
COPY tests/migration/deployment.yaml ./deployment.yaml
COPY tests/migration/scripts/run-migration.sh ./run-migration.sh
RUN chmod +x thunderid run-migration.sh

ENTRYPOINT ["/opt/thunderid/run-migration.sh"]
