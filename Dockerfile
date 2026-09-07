FROM library/alpine:latest@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS certs
RUN apk update && apk add ca-certificates

FROM library/busybox:1.38.0@sha256:dc2d74b28e4cf8984fa52af1f39bc7c3d9c73760b41a74d629f5d11b1ab28616
ARG TARGETPLATFORM
COPY --from=certs /etc/ssl/certs /etc/ssl/certs
COPY $TARGETPLATFORM/email-backup-tool /usr/bin/
CMD ["/usr/bin/email-backup-tool"]