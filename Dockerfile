FROM golang:1.23.0-alpine3.20 as builder

WORKDIR /src

RUN apk --update --no-cache add git make

ENV CGO_ENABLED=0

COPY go.mod go.mod
COPY go.sum go.sum
COPY Makefile Makefile

RUN go mod download

COPY *.go ./
COPY pkg/ pkg/

RUN make build

FROM alpine:3.18

RUN apk --update --no-cache add ca-certificates

COPY --from=builder /src/spotinst-metrics-exporter /spotinst-metrics-exporter

USER nobody

ENTRYPOINT ["/spotinst-metrics-exporter"]
