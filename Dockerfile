FROM golang:1.16 AS builder
WORKDIR /root/aws-cost
ADD . .
RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build .

FROM alpine:3.14
LABEL maintainer="sergey.kondrashov@jetbrains.com"
RUN apk add ca-certificates && mkdir /app
WORKDIR /app
COPY --from=builder /root/aws-cost/aws-cost /app
ENTRYPOINT ["/app/aws-cost"]
