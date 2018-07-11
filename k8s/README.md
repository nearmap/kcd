
# Deployment of kcd

kcd can be deployed using:

1. Kubect: yaml specs for Kubenetes configuration is [here](kubectl/README.md)
2. ktmpl: [ktmpl](https://github.com/jimmycuadra/ktmpl) is another simple yaml templating engine. The Kubenetes yaml specs for  kcd configuration is [here](ktmpl/README.md)
3. Helm: Helm chart spec is [here](helm/kcd) and helm package is avaialble [here](https://raw.githubusercontent.com/nearmap/kcd/master/k8s/helm/kcd-0.1.0.tgz)


# ContainerVersion: a Custom Kubernetes Resource 
ContainerVersion is essentially a custom kubernetes resource definition (CRD) which is controller by the kcd (a k8s controller). The spec of ContainerVersion is specified [here](kubectl/cv-crd.yaml). 

An example of CV resource is:
```yaml
apiVersion: custom.k8s.io/v1
kind: ContainerVersion
metadata:
  name: myapp-cv
spec:
  imageRepo: nearmap/myapp
  tag: dev
  pollIntervalSeconds: 5
  container: myapp
  selector:
    cvapp: myappcv
```

And an example creation of CV resource is:
```sh
cat <<EOF | kubectl create -f -
apiVersion: custom.k8s.io/v1
kind: ContainerVersion
metadata:
  name: myapp-cv
spec:
  imageRepo: nearmap/myapp
  tag: dev
  pollIntervalSeconds: 5
  container: myappapp-container
  selector:
    cvapp: photocv
EOF
```

When ContainerVersion CRD is defined using (or with helm):
```sh
kubectl apply -f cv-crd.yaml
```
and are avaialble on API server at following interface:
http://localhost:8001/apis/custom.k8s.io/v1/containerversions/
http://localhost:8001/apis/custom.k8s.io/v1/namespaces/myapp/containerversions

and using *kubectl*:
```sh
    kubectl get cv --all-namespaces
```




