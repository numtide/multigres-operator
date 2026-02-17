# Containerfile for multigres-operator

# Github workflow step anchore/scan-action scans only the final image
# sync this intermediate FROM reference with:
#   scan-intermediate-image.yaml
FROM --platform=$BUILDPLATFORM golang:1.25.7-alpine3.23 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build using Go's native cross-compiler (CGO_ENABLED=0).
# --platform=$BUILDPLATFORM on the FROM makes this stage run on the host arch
# (amd64) even when targeting arm64, avoiding slow QEMU emulation.
RUN CGO_ENABLED=0 \
	GOOS=${TARGETOS:-linux} \
	GOARCH=${TARGETARCH} \
	go build \
	-ldflags '-s -w -buildid=' \
	-trimpath -mod=readonly \
	-a -o manager \
	cmd/multigres-operator/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source = "https://github.com/numtide/multigres-operator"
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
