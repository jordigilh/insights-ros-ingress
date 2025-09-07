# Multi-stage Dockerfile following ros-ocp-backend patterns
# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:1.24 AS builder

WORKDIR /go/src/app
COPY . .
USER 0
RUN go get -d ./... && \
    go build -o insights-ros-ingress ./cmd/insights-ros-ingress && \
    echo "$(go version)" > go_version_details

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
WORKDIR /
RUN microdnf -y update \
    --disableplugin=subscription-manager
COPY --from=builder /go/src/app/insights-ros-ingress ./insights-ros-ingress
COPY --from=builder /go/src/app/go_version_details ./go_version_details
EXPOSE 8080
USER 1001
CMD ["./insights-ros-ingress"]