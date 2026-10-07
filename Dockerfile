# Override GO_VERSION to your supported, patched toolchain when building releases.
ARG GO_VERSION=1.27.1
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gateway /gateway
COPY deploy/container.json /etc/gateway/config.json
EXPOSE 8080
ENTRYPOINT ["/gateway"]
CMD ["-config", "/etc/gateway/config.json"]
