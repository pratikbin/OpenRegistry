FROM golang:alpine as build
LABEL org.opencontainers.image.source = "https://github.com/jay-dee7/OpenRegistry"

WORKDIR /root/openregistry

COPY . .

RUN apk add gcc make git curl ca-certificates && go mod download github.com/NebulousLabs/go-skynet/v2 && make mod-fix && go mod download

RUN CGO_ENABLED=0 go build -o openregistry -ldflags="-w -s" main.go

FROM alpine:latest

COPY --from=build /root/openregistry/openregistry .
COPY ./config.yaml .
EXPOSE 80
CMD ["./openregistry"]
