# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/callio ./cmd/callio

FROM alpine:3.22
RUN apk add --no-cache ca-certificates \
    && addgroup -S callio \
    && adduser -S -G callio callio
WORKDIR /app
COPY --from=build /out/callio ./callio
COPY web ./web

ENV PORT=8080 \
    CALLIO_BIND_HOST=0.0.0.0
EXPOSE 8080
USER callio
ENTRYPOINT ["./callio"]
