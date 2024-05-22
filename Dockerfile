FROM golang:1.19 AS builder

WORKDIR /go/src/github.com/sapcc/kube-detective
ADD . .
RUN --mount=type=cache,target=/go/pkg/mod \
	  --mount=type=cache,target=/root/.cache/go-build \
		make

FROM alpine:3.20
LABEL source_repository="https://github.com/sapcc/kube-detective"
RUN apk add --no-cache \
        ca-certificates \
        wget
COPY --from=builder /go/src/github.com/sapcc/kube-detective/bin/linux/amd64/kube-detective /kube-detective
ENTRYPOINT ["/kube-detective"]
