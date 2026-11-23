FROM golang:1.24-alpine AS build

# Tools are built from the wc-tools repository, passed as a named build context:
#   docker build --build-context wctools=../wc-tools -t wc-harness:dev .
WORKDIR /src/tools
COPY --from=wctools . .
RUN sh build-tools.sh /out/tools

WORKDIR /src/harness
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wc-harness ./cmd/wc-harness

FROM alpine:3.21

RUN apk add --no-cache bash ca-certificates \
    && adduser -D -u 10001 -h /home/wc wc \
    && mkdir -p /workspace \
    && chown wc:wc /workspace

COPY --from=build /out/tools /home/wc/tools
COPY --from=build /out/wc-harness /usr/local/bin/wc-harness
RUN chown -R wc:wc /home/wc/tools

WORKDIR /workspace
USER wc
ENTRYPOINT ["/usr/local/bin/wc-harness", "-tools", "/home/wc/tools"]
