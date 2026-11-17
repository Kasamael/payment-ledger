# Build on the runner's own platform and cross-compile for the target:
# much faster than compiling Go under QEMU for linux/arm64.
FROM --platform=$BUILDPLATFORM mirror.gcr.io/library/golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/ledger ./cmd/ledger

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/ledger /ledger

EXPOSE 8080

ENTRYPOINT ["/ledger"]
