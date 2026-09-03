# Fallback container for the devboard dashboard. The primary deployment is
# the systemd user unit running `worklog serve` natively (see README);
# this image exists for setups that want supervision via Docker.
# Build context is the repo root: docker compose -f devboard/compose.yaml up
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY worklog/ .
RUN CGO_ENABLED=0 go build -o /out/worklog ./cmd/worklog

FROM alpine
COPY --from=build /out/worklog /usr/local/bin/worklog
ENV DEVBOARD_DATA=/data DEVBOARD_WORKLOG=/worklog DEVBOARD_PORT=8484
EXPOSE 8484
CMD ["worklog", "serve"]
