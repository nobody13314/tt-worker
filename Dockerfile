FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tt-worker ./cmd/tt-worker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
COPY --from=build /out/tt-worker /usr/local/bin/tt-worker
USER nobody
ENTRYPOINT ["/usr/local/bin/tt-worker"]
