FROM golang:1.16.5 AS build

WORKDIR /opt
ADD *.go go.* /opt/
RUN go build

FROM scratch

COPY --from=build /opt/enphase-envoy-local-monitoring /bin/
USER 500:500
ENTRYPOINT ["/bin/enphase-envoy-local-monitoring"]
