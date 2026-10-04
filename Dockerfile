FROM library/alpine:latest@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS certs
RUN apk update && apk add ca-certificates

FROM library/busybox:1.38.0@sha256:dc2d74b28e4cf8984fa52af1f39bc7c3d9c73760b41a74d629f5d11b1ab28616
ARG TARGETPLATFORM
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY $TARGETPLATFORM/email-backup-tool /usr/bin/
CMD ["/usr/bin/email-backup-tool"]