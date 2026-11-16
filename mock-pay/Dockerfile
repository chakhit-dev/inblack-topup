FROM golang:1.27-bookworm AS builder
WORKDIR /app
COPY main.go .
RUN go mod init mock-pay && CGO_ENABLED=0 go build -o mock-pay main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/mock-pay .
EXPOSE 9000
CMD ["./mock-pay"]