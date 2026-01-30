FROM golang:1.25.6-alpine3.23 AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum* ./
RUN go mod download

COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kafka-app main.go

FROM alpine:3.23

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/kafka-app .

CMD ["./kafka-app"]
