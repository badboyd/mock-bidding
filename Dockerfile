FROM golang:1.12-alpine3.9 as builder
RUN apk update
RUN apk add --no-cache git
RUN go get -u github.com/golang/dep/cmd/dep
WORKDIR /go/src/github.com/badboyd/mock-bidding/
COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure -v -vendor-only
COPY . /go/src/github.com/badboyd/mock-bidding/
RUN go build -o ./dist/exe

FROM alpine:3.9
RUN apk add --update ca-certificates
RUN apk add --no-cache tzdata && \
  cp -f /usr/share/zoneinfo/Asia/Ho_Chi_Minh /etc/localtime && \
  apk del tzdata
WORKDIR /app
COPY --from=builder /go/src/github.com/badboyd/mock-bidding/dist/exe .
ENTRYPOINT ["./exe"]
