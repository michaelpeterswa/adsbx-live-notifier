# -=-=-=-=-=-=- Compile Image -=-=-=-=-=-=-

FROM golang:1 AS stage-compile

ARG VERSION=dev

WORKDIR /go/src/app
COPY . .

# hadolint ignore=DL3062
RUN go get -d -v ./... && \
    CGO_ENABLED=0 GOOS=linux go build \
        -ldflags="-s -w -X main.version=${VERSION}" \
        -o adsbx-live-notifier \
        ./cmd/adsbx-live-notifier

# -=-=-=-=- Final Distroless Image -=-=-=-=-

# hadolint ignore=DL3007
FROM gcr.io/distroless/static-debian12:latest AS stage-final

COPY --from=stage-compile /go/src/app/adsbx-live-notifier /
CMD ["/adsbx-live-notifier"]
