FROM golang:alpine


ADD . /go/src/github.com/nearmap/cvmanager
RUN go install github.com/nearmap/cvmanager

RUN rm -r /go/src/github.com/nearmap/cvmanager

VOLUME /go/src

RUN mkdir -p /health/ && \
	chmod 0777 /health/

RUN mkdir -p /cvmanager
ADD ./k8s /cvmanager/
ADD version /cvmanager/

WORKDIR /cvmanager

EXPOSE 2019

ENTRYPOINT ["cvmanager"]
