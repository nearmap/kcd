# Deploy kcd using helm

## Setup
Use [helm](https://github.com/kubernetes/helm/) Kubernetes package manager to install kcd into kube cluster.

## Install kcd
> To run kcd with RBAC Role, use `useRBAC` as false.
### Dry run
```
helm install --dry-run --name kcdapp --debug ./kcd \
    --set service.type=NodePort --namespace kube-system
```

### Install
```
 helm install --name kcdapp ./kcd \
    --set service.type=NodePort --namespace kube-system
```

### Delete
```
helm delete kcdapp --purge  
```
