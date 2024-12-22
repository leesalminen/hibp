FROM golang:1.21-alpine3.18 as builder
RUN apk add alpine-sdk ca-certificates

WORKDIR /go/src/github.com/leesalminen/hibp
COPY . .

RUN go mod download
RUN go mod verify

RUN make build

FROM alpine:3.13
COPY --from=builder /etc/ca-certificates /etc/ca-certificates
COPY --from=builder /go/src/github.com/leesalminen/hibp/hibp-linux-amd64 /opt/hibp/bin/hibp-linux-amd64
ENTRYPOINT ["/opt/hibp/bin/hibp-linux-amd64"]
CMD ["--help"]