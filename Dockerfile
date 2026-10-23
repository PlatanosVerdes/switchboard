FROM golang:1.26-alpine AS build

WORKDIR /src
ARG VERSION=local
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Static binary: the runtime image has no libc to link against.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.buildVersion=${VERSION}" -o /out/switchboard ./cmd/switchboard

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && adduser -D -H -u 10001 switchboard
COPY --from=build /out/switchboard /usr/local/bin/switchboard

ENV SWITCHBOARD_LOG_JSON=1 \
    SWITCHBOARD_PORT=8000

EXPOSE 8000
USER switchboard
ENTRYPOINT ["switchboard"]
