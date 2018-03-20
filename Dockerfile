FROM nearmap/golang

ARG DEBIAN_FRONTEND=noninteractive

ADD . /go/src/github.com/nearmap/cvmanager
RUN go install github.com/nearmap/cvmanager

RUN rm -r /go/src/github.com/nearmap/cvmanager

VOLUME /go/src

EXPOSE 2019

ENTRYPOINT ["cvmanager"]
