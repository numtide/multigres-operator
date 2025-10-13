# Containerfile for multigres-operator

# Github workflow step anchore/scan-action scans only the final image
# sync this intermediate FROM reference with:
#   build-and-release.yaml => scan-intermediate-image
FROM --platform=$BUILDPLATFORM alpine:3.22.2 AS build

ARG TARGETOS
ARG TARGETARCH

COPY dist dist
RUN cp dist/multigres-operator-${TARGETARCH}/multigres-operator-${TARGETARCH} multigres-operator
RUN chmod +x multigres-operator

FROM gcr.io/distroless/static-debian12

COPY --from=build multigres-operator multigres-operator

ENTRYPOINT [ "./multigres-operator" ]