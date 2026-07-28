FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY *.go ./
RUN go build -o stwb-openmensa .

FROM alpine:3.19

RUN adduser -D -u 1000 app && mkdir -p /var/lib/stwb-openmensa && chown app:app /var/lib/stwb-openmensa
USER app

COPY --from=builder /app/stwb-openmensa /usr/local/bin/stwb-openmensa

EXPOSE 8080
VOLUME ["/var/lib/stwb-openmensa"]

ENTRYPOINT ["stwb-openmensa", "-listen", "0.0.0.0", "-cache-dir", "/var/lib/stwb-openmensa"]
