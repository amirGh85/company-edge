FROM golang:1.26-alpine AS build
WORKDIR /src
COPY control-panel/go.mod ./go.mod
RUN go mod download gopkg.in/yaml.v3
COPY control-panel/main.go ./main.go
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/control-panel ./main.go

FROM alpine:3.22
RUN addgroup -S panel && adduser -S -G panel panel
WORKDIR /app
RUN mkdir -p /app/bin
COPY --from=build /out/control-panel /app/bin/control-panel
COPY control-panel/static /app/control-panel/static
USER panel
ENV CONTROL_PANEL_ADDR=0.0.0.0:8090
EXPOSE 8090
ENTRYPOINT ["/app/bin/control-panel"]
