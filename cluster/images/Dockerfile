FROM golang:1.13

WORKDIR /build
COPY go.mod go.sum /build/
RUN go mod download

COPY cmd /build/cmd
COPY pkg /build/pkg

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o crossplane-agent cmd/main.go
# ---
FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=0 /build/crossplane-agent /usr/local/bin/

USER 1001
ENTRYPOINT ["crossplane-agent"]
