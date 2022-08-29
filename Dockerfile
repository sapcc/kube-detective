FROM golang:1.19.0 AS builder

WORKDIR /go/src/github.com/sapcc/kube-detective
ADD go.mod go.sum ./
RUN go mod download
ADD . .
RUN make

FROM ubuntu
LABEL source_repository="https://github.com/sapcc/kube-detective"
COPY --from=builder /go/src/github.com/sapcc/kube-detective/bin/linux/amd64/kube-detective /kube-detective
