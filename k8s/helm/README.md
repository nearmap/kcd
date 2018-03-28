# Deploy CVManager using helm

## Setup
Use [helm](https://github.com/kubernetes/helm/) Kubernetes package manager to install CVManager into kube cluster.

## Install CVManager
### Dry run
```
helm install --dry-run --name cvmanagerapp --debug ./cvmanager \
    --set service.type=NodePort --namespace kube-system
```

### Install
```
 helm install --name cvmanagerapp ./cvmanager \
    --set service.type=NodePort --namespace kube-system
```

### Delete
```
helm delete cvmanagerapp --purge  
```
