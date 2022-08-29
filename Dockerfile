FROM golang:1.19 AS builder

WORKDIR /go/src/github.com/sapcc/kube-detective
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN make

FROM alpine:3.16
LABEL source_repository="https://github.com/sapcc/kube-detective"
RUN apk add --no-cache \
        ca-certificates \
        wget
RUN wget -q https://storage.googleapis.com/kubernetes-release/release/v1.23.6/bin/linux/amd64/kubectl && \
        chmod 744 kubectl && \
        mv kubectl /usr/bin/kubectl
COPY --from=builder /go/src/github.com/sapcc/kube-detective/bin/linux/amd64/kube-detective /kube-detective
ENTRYPOINT ["/kube-detective"]
