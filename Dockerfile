FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" -o /out/otlpforge .

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/otlpforge /app/otlpforge
EXPOSE 8080
ENTRYPOINT ["/app/otlpforge"]
