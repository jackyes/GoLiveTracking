FROM golang:latest AS builder
WORKDIR /app
COPY . .
RUN apt update && apt install -y git gcc build-essential
RUN go get -u -v all
RUN go mod download
RUN CGO_ENABLED=1 go build -o GOLiveTracking .

FROM debian:latest
RUN apt update && apt full-upgrade -y && rm -rf /var/cache/*
WORKDIR /root/
COPY . .
COPY --from=builder /app/GOLiveTracking .

RUN adduser --disabled-password --gecos "" appuser
USER appuser

EXPOSE 8080
CMD ["./GOLiveTracking"]
