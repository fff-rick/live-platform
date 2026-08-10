FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/live-api ./cmd/api \
    && CGO_ENABLED=0 go build -o /out/live-worker ./cmd/worker

FROM alpine:3.21
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/live-api /usr/local/bin/live-api
COPY --from=build /out/live-worker /usr/local/bin/live-worker
CMD ["live-api"]
