FROM golang:alpine

ADD . /go/src/github.com/eric1313/kcd
RUN go install github.com/eric1313/kcd

RUN rm -r /go/src/github.com/eric1313/kcd

VOLUME /go/src

# TODO: this is dodgy it expects k8s files to always be available from runtime directory

# need to package the yaml version file using tool chains properly
RUN mkdir -p /kcd
ADD ./k8s /kcd/k8s/
#ADD version /kcd/

WORKDIR /kcd

EXPOSE 2019

USER 1001

ENTRYPOINT ["kcd"]
