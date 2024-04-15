# syntax=docker/dockerfile:1

FROM golang:1.22-bookworm as build
WORKDIR /build

RUN mkdir -p /out

COPY go.* ./
RUN go mod download

COPY . ./
RUN go build -race -o /out/contourguessr-api .

FROM debian:bookworm
LABEL org.opencontainers.image.source="https://github.com/dzfranklin/contourguessr-api"

ARG APP_ENV=prod
ARG HOST="0.0.0.0"
ARG PORT="8080"

RUN mkdir -p /app
COPY --from=build /out/contourguessr-api /app/
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENV TZ=UTC
ENV APP_ENV=$APP_ENV
ENV HOST=$HOST
ENV PORT=$PORT
ENTRYPOINT ["/app/contourguessr-api"]
