# Deploy CVManager using kubectl

## Setup
Uses [ktmpl](https://github.com/jimmycuadra/ktmpl) based [tool](https://hub.docker.com/r/nearmap/nktmpl/) for yaml templating and kubectl to intract with Kubernetes cluster.

## Custom resource definition (CRD)

- ContainerVersion [CR](cv-crd.yaml)

## Container version

```sh
kubectl apply -f  k8s/cv-crd.yaml
```

On API server these are assessible by:
http://localhost:8001/apis/custom.k8s.io/v1/containerversions/
http://localhost:8001/apis/custom.k8s.io/v1/namespaces/photos/containerversions

and using *kubectl*:
```sh
    kubectl get cv --all-namespaces
```


## Deploy CVManager
*Run*
```
docker run -ti -v `pwd`:/config nearmap/nktmpl Backend.yaml --parameter REGION ap-southeast-2  --parameter VERSION <SHA> |  kubectl apply -f -
```

*Delete*
```
docker run -ti -v `pwd`:/config nearmap/nktmpl Backend.yaml --parameter REGION ap-southeast-2  --parameter VERSION <SHA> |  kubectl delete -f -
```

