FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o /bot ./cmd/bot

FROM gcr.io/distroless/static-debian12
COPY --from=build /bot /bot
ENTRYPOINT ["/bot", "--config", "/config/config.yaml"]
