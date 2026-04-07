FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY *.go ./
RUN go build -o stwb-openmensa .

FROM alpine:3.19

RUN adduser -D -u 1000 app
USER app

COPY --from=builder /app/stwb-openmensa /usr/local/bin/stwb-openmensa

EXPOSE 8080

ENTRYPOINT ["stwb-openmensa", "-listen", "0.0.0.0"]
