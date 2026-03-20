FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod .
COPY main.go .
RUN go build -o /openai-stub .

FROM alpine:3.19
COPY --from=builder /openai-stub /openai-stub
EXPOSE 8090
CMD ["/openai-stub"]
