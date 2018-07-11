# Deploy kcd using ktmpl

## Setup
Uses [ktmpl](https://github.com/jimmycuadra/ktmpl) based [tool](https://hub.docker.com/r/nearmap/nktmpl/) for yaml templating and kubectl to intract with Kubernetes cluster.



## Deploy kcd and with RBAC

*Run*

```sh
    # Apply rbac roles
    kubectl apply -f rbac-role.yaml
    # Provision KCD with dedicated service account kcd
    docker run -ti -v `pwd`:/config nearmap/nktmpl kcd.yaml --parameter VERSION <SHA>  --parameter SVC_ACCOUNT kcd | kubectl apply -f -
```

*Delete*
```sh
    docker run -ti -v `pwd`:/config nearmap/nktmpl kcd.yaml --parameter REGION ap-southeast-2  --parameter VERSION <SHA> --parameter SVC_ACCOUNT kcd |  kubectl delete -f -
```


## Deploy kcd and without RABC i.e. with default service account

*Run*

```sh
    docker run -ti -v `pwd`:/config nearmap/nktmpl kcd.yaml --parameter VERSION <SHA> |  kubectl apply -f -
```

*Delete*
```sh
    docker run -ti -v `pwd`:/config nearmap/nktmpl kcd.yaml --parameter REGION ap-southeast-2  --parameter VERSION <SHA> |  kubectl delete -f -
```


## Remove CV resource def
```sh
    kubectl  get crd cv 
```
