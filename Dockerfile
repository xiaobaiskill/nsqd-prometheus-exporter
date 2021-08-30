FROM golang:1.16.7-alpine3.14 as builder

LABEL stage=builder
RUN go env -w GOPROXY="https://goproxy.cn,direct"
WORKDIR /go/src/app
ARG Version=""
ADD . .
RUN CGO_ENABLED=0 GOOS=linux go build -o nsqd-prometheus-exporter  -a -installsuffix cgo -ldflags \
    "-X 'main.Version=${Version}'" .

FROM alpine:3.14
WORKDIR /
COPY --from=builder /go/src/app/nsqd-prometheus-exporter .
ENTRYPOINT ["/nsqd-prometheus-exporter"]
