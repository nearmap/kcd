
# Deployment of cvmanager

CVManager can be deployed using:

1. Kubectl: yaml specs for Kubenetes configuration is [here](kubectl/README.md)
2. Helm: Helm chart spec is [here](helm/cvmanager) and helm package is avaialble [here](https://raw.githubusercontent.com/nearmap/cvmanager/master/k8s/helm/cvmanager/cvmanager-0.1.0.tgz)


# ContainerVersion: a Custom Kubernetes Resource 
ContainerVersion is essentially a custom kubernetes resource definition (CRD) which is controller by the CVManager (a k8s controller). The spec of ContainerVersion is specified [here](kubectl/cv-crd.yaml). 

An example of CV resource is:
```yaml
apiVersion: custom.k8s.io/v1
kind: ContainerVersion
metadata:
  name: photos-cv
spec:
  imageRepo: nearmap/photos
  tag: dev
  checkFrequency: 5
  container: photosapp-container
  selector:
    cvapp: photocv
```

And an example creation of CV resource is:
```sh
cat <<EOF | kubectl create -f -
apiVersion: custom.k8s.io/v1
kind: ContainerVersion
metadata:
  name: photos-cv
spec:
  imageRepo: nearmap/photos
  tag: dev
  checkFrequency: 5
  container: photosapp-container
  selector:
    cvapp: photocv
EOF
```

When ContainerVersion CRD is defined using (or with helm):
```sh
kubectl apply -f  kubectl/cv-crd.yaml
```
and are avaialble on API server at following interface:
http://localhost:8001/apis/custom.k8s.io/v1/containerversions/
http://localhost:8001/apis/custom.k8s.io/v1/namespaces/photos/containerversions

and using *kubectl*:
```sh
    kubectl get cv --all-namespaces
```




