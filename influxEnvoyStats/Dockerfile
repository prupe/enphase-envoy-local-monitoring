FROM golang:1.16.5 AS build

WORKDIR /opt
ADD *.go go.* /opt/
RUN go build

FROM scratch

COPY --from=build /etc/passwd /etc/group /etc/ssl /etc/
COPY --from=build /usr/lib/ssl /usr/lib/
COPY --from=build /opt/influxEnvoyStats /bin/

USER nobody:nogroup
ENTRYPOINT ["/bin/influxEnvoyStats"]
