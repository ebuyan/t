FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o tportfolio .

FROM alpine:3.21

# ca-certificates — для HTTPS-запросов к invest-public-api.tbank.ru
RUN apk add --no-cache ca-certificates

COPY --from=builder /app/tportfolio /tportfolio

ENTRYPOINT ["/tportfolio"]
