FROM golang:1.16.6 AS build

WORKDIR /opt
ADD go.* /opt/
RUN go mod download
ADD *.go /opt/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

FROM scratch

COPY --from=build /etc/passwd /etc/group /etc/ssl /etc/
COPY --from=build /usr/lib/ssl /usr/lib/
COPY --from=build /opt/influxEnvoyStats /bin/

USER nobody:nogroup
ENTRYPOINT ["/bin/influxEnvoyStats"]
