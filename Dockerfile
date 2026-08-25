FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-commerce ./cmd/commerce \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-interaction ./cmd/interaction \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-identity-room ./cmd/identityroom \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/live-migrate ./cmd/migrate

FROM alpine:3.24.1
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/live-api /usr/local/bin/live-api
COPY --from=build /out/live-commerce /usr/local/bin/live-commerce
COPY --from=build /out/live-interaction /usr/local/bin/live-interaction
COPY --from=build /out/live-identity-room /usr/local/bin/live-identity-room
COPY --from=build /out/live-worker /usr/local/bin/live-worker
COPY --from=build /out/live-migrate /usr/local/bin/live-migrate
COPY migrations /app/migrations
COPY svg /app/svg
USER 10001:10001
CMD ["live-api"]
