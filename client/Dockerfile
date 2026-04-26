# build & run locally with:
#   cd "$(git rev-parse --show-toplevel)"
#   CGO_ENABLED=0 go build -o openzro ./client
#   sudo podman build -t localhost/openzro:latest -f client/Dockerfile --ignorefile .dockerignore-client .
#   sudo podman run --rm -it --cap-add={BPF,NET_ADMIN,NET_RAW} localhost/openzro:latest

FROM alpine:3.22.0
# iproute2: busybox doesn't display ip rules properly
RUN apk add --no-cache \
    bash \
    ca-certificates \
    ip6tables \
    iproute2 \
    iptables

ENV \
    OPENZRO_BIN="/usr/local/bin/openzro" \
    OZ_LOG_FILE="console,/var/log/openzro/client.log" \
    OZ_DAEMON_ADDR="unix:///var/run/openzro.sock" \
    OZ_ENTRYPOINT_SERVICE_TIMEOUT="5" \
    OZ_ENTRYPOINT_LOGIN_TIMEOUT="1"

ENTRYPOINT [ "/usr/local/bin/openzro-entrypoint.sh" ]

ARG OPENZRO_BINARY=openzro
COPY client/openzro-entrypoint.sh /usr/local/bin/openzro-entrypoint.sh
COPY "${OPENZRO_BINARY}"  /usr/local/bin/openzro
