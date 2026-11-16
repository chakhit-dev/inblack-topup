FROM golang:1.27-bookworm AS builder
WORKDIR /app
COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -o /mock-game main.go

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=builder /mock-game /app/mock-game
EXPOSE 9001
CMD ["/app/mock-game"]