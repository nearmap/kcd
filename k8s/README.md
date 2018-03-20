
# Custom resource definition (CRD)

Contains CRDs:
- ContainerVersion [CR](cv-crd.yaml)

## Container version

```sh
kubectl apply -f  k8s/cv-crd.yaml
```

#### example
```sh
kubectl apply -f k8s/sample-cv.yaml  
```


On API server these are assessible by:
http://localhost:8001/apis/custom.k8s.io/v1/containerversions/
http://localhost:8001/apis/custom.k8s.io/v1/namespaces/photos/containerversions

and using *kubectl*:
```sh
    kubectl get cv --all-namespaces --v=8
```


# Deploy cvmanager
## Configuration on k8s cluster
Uses [ktmpl](https://github.com/jimmycuadra/ktmpl) based [tool](https://hub.docker.com/r/nearmap/nktmpl/) for yaml templating:

## Deploy controller service
*Run*
```
docker run -ti -v `pwd`:/config nearmap/nktmpl Backend.yaml --parameter ENV dev --parameter VERSION <SHA> |  kubectl apply -f -
```

*Delete*
```
docker run -ti -v `pwd`:/config nearmap/nktmpl Backend.yaml --parameter ENV dev --parameter VERSION <SHA> |  kubectl delete -f -
```

