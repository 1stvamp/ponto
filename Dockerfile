# syntax=docker/dockerfile:1

ARG BUN_VERSION=1.3
ARG GO_VERSION=1.24
ARG TF_VERSION=1.5.5

# Build UI (bun, no Node)
FROM --platform=$BUILDPLATFORM oven/bun:${BUN_VERSION}-alpine AS ui
WORKDIR /src
COPY ./ui/package.json ./ui/bun.lock ./
RUN bun install --frozen-lockfile
COPY ./ui/index.html ./ui/vite.config.js ./
COPY ./ui/public ./public
COPY ./ui/src ./src
RUN bun --bun vite build

# Build the ponto binary
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/dist ./ui/dist
ARG TARGETOS TARGETARCH
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /out/ponto .

# UPX-compress the binary for the slim image
FROM build AS build-slim
RUN apk add --no-cache upx && upx -q --best /out/ponto

# Standard image: terraform base + chromium (supports -genImage)
FROM hashicorp/terraform:${TF_VERSION} AS standard
RUN apk add --no-cache chromium ca-certificates
COPY --from=build /out/ponto /bin/ponto
WORKDIR /src
ENTRYPOINT ["/bin/ponto"]

# Slim image: scratch + UPX'd terraform and ponto (no chromium; no -genImage)
FROM hashicorp/terraform:${TF_VERSION} AS tf-slim
RUN apk add --no-cache upx && cp /bin/terraform /terraform && upx -q --best /terraform

FROM scratch AS slim
COPY --from=standard /etc/ssl/certs/ /etc/ssl/certs/
COPY --from=tf-slim /terraform /bin/terraform
COPY --from=build-slim /out/ponto /bin/ponto
WORKDIR /src
ENTRYPOINT ["/bin/ponto"]
