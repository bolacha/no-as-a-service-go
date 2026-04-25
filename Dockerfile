FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o no-as-a-service .

FROM scratch
COPY --from=builder /app/no-as-a-service /no-as-a-service
EXPOSE 3000
ENTRYPOINT ["/no-as-a-service"]
