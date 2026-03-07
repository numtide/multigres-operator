FROM --platform=$BUILDPLATFORM golang:1.25.7-alpine3.23 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
	GOOS=${TARGETOS:-linux} \
	GOARCH=${TARGETARCH} \
	go build \
	-ldflags "-s -w -buildid= \
	-X main.version=${VERSION} \
	-X main.gitCommit=${GIT_COMMIT} \
	-X main.buildDate=${BUILD_DATE}" \
	-trimpath -mod=readonly \
	-a -o multigres-observer \
	cmd/multigres-observer/main.go

FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source="https://github.com/numtide/multigres-operator"
WORKDIR /
COPY --from=builder /workspace/multigres-observer .
USER 65532:65532

ENTRYPOINT ["/multigres-observer"]
