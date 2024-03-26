# Description: Dockerfile for building the go application

FROM docker.io/library/golang:1.22 as builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# From another image download wal-g binary
FROM docker.io/library/alpine:latest as wal-g

ARG WALG_VERSION=1.1

RUN apk --no-cache add ca-certificates
WORKDIR /usr/local/bin
RUN wget https://github.com/wal-g/wal-g/releases/download/v${WALG_VERSION}/wal-g-pg-ubuntu-20.04-amd64 -O wal-g
RUN chmod +x wal-g

# Create a minimal ubuntu 20.04 image
FROM docker.io/library/ubuntu:20.04

RUN apt-get update && \
    apt-get install -y \
      ca-certificates \
      daemontools

COPY --from=wal-g /usr/local/bin/wal-g /usr/local/bin/wal-g

# Create app directory
WORKDIR /app

RUN ln -s /vault/secrets/wal-g-exporter.env .env
COPY --from=builder /app/main ./

# This container exposes port 9099 to the outside world
EXPOSE 9099

# Command to run the executable
CMD ["./main"]
