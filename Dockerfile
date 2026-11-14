FROM node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine@sha256:30b1e546a9c7fa3b747c2bdba3a0c7bdf3a02ebb4c5b8405d2f948d7e8aac4ec AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -trimpath -o /out/onbo ./cmd/onbo && CGO_ENABLED=0 go build -trimpath -o /out/mock ./cmd/mock && CGO_ENABLED=0 go build -trimpath -o /out/eval ./cmd/eval

FROM alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2
RUN apk add --no-cache ca-certificates && addgroup -S onbo && adduser -S onbo -G onbo
WORKDIR /app
COPY --from=go /out/ /app/
COPY --from=web /src/web/dist /app/web/dist
COPY configs/ /app/configs/
USER onbo
ENV HTTP_ADDR=:8080 STATIC_DIR=/app/web/dist
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/onbo"]
