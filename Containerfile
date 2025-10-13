FROM --platform=$BUILDPLATFORM alpine:3.22.2 AS build
ARG TARGETOS
ARG TARGETARCH

COPY dist dist
RUN cp dist/multigres-operator-${TARGETARCH}/multigres-operator-${TARGETARCH} multigres-operator
RUN chmod +x multigres-operator

FROM alpine:3.22.2

COPY --from=build multigres-operator multigres-operator

ENTRYPOINT [ "./multigres-operator" ]