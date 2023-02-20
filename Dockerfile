FROM golang:1.20.1-bullseye as builder
WORKDIR /app
COPY . .
RUN go mod download
RUN go build GOLiveTracking.go
FROM debian:stable-slim
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get upgrade && rm -rf /var/lib/apt/lists/*
COPY . .
COPY --from=builder /app/GOLiveTracking /app/GOLiveTracking
EXPOSE 8080
CMD ["./app/GOLiveTracking"]
