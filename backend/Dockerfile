FROM golang:1.26-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /usr/local/bin/rapid-api ./cmd/api

FROM alpine:3.22

RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=build /usr/local/bin/rapid-api /usr/local/bin/rapid-api

ENV API_ADDR=:8080
EXPOSE 8080

USER appuser
CMD ["rapid-api"]
