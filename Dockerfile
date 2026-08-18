FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-migrate ./cmd/migrate

FROM alpine:3.24.1
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/live-api /usr/local/bin/live-api
COPY --from=build /out/live-worker /usr/local/bin/live-worker
COPY --from=build /out/live-migrate /usr/local/bin/live-migrate
COPY migrations /app/migrations
COPY svg /app/svg
USER 10001:10001
CMD ["live-api"]
