# Build the manager binary
FROM golang:1.19 as builder

WORKDIR /build

# Copy the Go Modules manifests
COPY go.mod go.mod
#COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go main.go

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o server main.go

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /build/server .
USER nonroot:nonroot

ENTRYPOINT ["/server"]