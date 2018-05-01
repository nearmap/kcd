# Deploy CVManager using ktmpl

## Setup
Uses [ktmpl](https://github.com/jimmycuadra/ktmpl) based [tool](https://hub.docker.com/r/nearmap/nktmpl/) for yaml templating and kubectl to intract with Kubernetes cluster.


## Deploy CVManager and all required resources

*Run*

```sh
    docker run -ti -v `pwd`:/config nearmap/nktmpl cvmanager.yaml --parameter VERSION <SHA> |  kubectl apply -f -
```

*Delete*
```sh
    docker run -ti -v `pwd`:/config nearmap/nktmpl cvmanager.yaml --parameter REGION ap-southeast-2  --parameter VERSION <SHA> |  kubectl delete -f -
```
Remove CV resource def
```sh
    kubectl  get crd cv 
```
