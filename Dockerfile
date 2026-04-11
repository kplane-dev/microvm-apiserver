FROM golang:1.25 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /microvm-apiserver ./cmd/microvm-apiserver

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=builder /microvm-apiserver /usr/local/bin/microvm-apiserver
ENTRYPOINT ["microvm-apiserver"]
