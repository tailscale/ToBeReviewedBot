FROM golang:1.19.0-alpine3.16 as builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN go build


FROM alpine:3.16 as tailscale
WORKDIR /app
ENV TSFILE=tailscale_1.28.0_amd64.tgz
RUN wget https://pkgs.tailscale.com/stable/${TSFILE} && tar xzf ${TSFILE} --strip-components=1


# https://docs.docker.com/develop/develop-images/multistage-build/#use-multi-stage-builds
FROM alpine:3.16
RUN apk update && apk add ca-certificates iptables ip6tables && rm -rf /var/cache/apk/*

# Copy binary to production image
COPY --from=builder /app/start.sh /start.sh
COPY --from=builder /app/ToBeReviewedBot /ts-tbrbot
COPY --from=tailscale /app/tailscaled /usr/local/bin/tailscaled
COPY --from=tailscale /app/tailscale /usr/local/bin/tailscale
RUN mkdir -p /var/run/tailscale /var/cache/tailscale /var/lib/tailscale

# Run on container startup.
CMD ["/start.sh"]
