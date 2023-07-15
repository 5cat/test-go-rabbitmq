FROM golang:1.20 AS build-stage

ARG BUILD_PATH
WORKDIR /app/${BUILD_PATH}

COPY ${BUILD_PATH} ./

RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -o /pikapo

# Deploy the application binary into a lean image
FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

COPY --from=build-stage /pikapo /pikapo

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/pikapo"]
