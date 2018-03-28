[![CircleCI](https://circleci.com/gh/nearmap/cvmanager.svg?style=svg&circle-token=e635659d5d8190eb041cc92149262a5b75470fcd)](https://circleci.com/gh/nearmap/cvmanager)[![Code Quality](https://goreportcard.au-api.nearmap.com/badge/github.com/nearmap/cvmanager)](https://goreportcard.au-api.nearmapdev.com/report/github.com/nearmap/cvmanager)

# CVManager
Container Version Manager (cvmanager) is a continous integration (CI) and continous delivery (CD) tool designed for Kubernetes cluster/services. Fundamentally, cvmanager is a custom Kubernetes controller to achieve a declarative configuration approach to continuous deployment. 

Deployments that requires CI/CD, can declare [ContainerVersion](k8s/cv-crd.yaml) resource. [CVManager](k8s/Backend.yaml), ContainerVersion controller starts monitoring for any new changes that should be rolled-out. If so, using the rollout strategy specified in this deployment, the rollout of new version is carried out.

CVManager assumes ECR as the container registry. Supporting other registeries is T2D.

The tool has 3 main parts:
- CV Manager
- Docker Registry Syncer (supports ECR and Dockerhub)
- Docker Registry Tagger (supports ECR, with limited Dockerhub support)

Docker images are on [docker.io](https://hub.docker.com/r/nearmap/cvmanager/)

## CVManager: Controller service
ContainerVersion controller that manages ContainerVersion resources.

### Run locally
```
 REGION=us-east-1 AWS_DEFAULT_REGION=us-east-1 VERSION=3ae8d1b85862921af30de6d0461d2e73086f5961  cvmanager run \
    --k8s-config ~/.kube/config 
```

## Docker registry sync service

Registry sync service is a polling service that frequently check on registry (AWS ECR and dockerhub only) to see if new version should be rolled out for a given deployment/container.

Sync service default to using AWS ECR as regisrty provider but dockerhub is also supported. Use ```--provider dockerhub``` to use syncer service against dockerhub repo.

Dockerhub *note*: 
Dockerhub has very limited support w.r.t. tags via API and also multi-tag support is very limited. see [1](https://github.com/kubernetes/kubernetes/issues/33664), [2](https://github.com/kubernetes/kubernetes/issues/11348), [3](https://github.com/docker/hub-feedback/issues/68) and [4](https://github.com/kubernetes/kubernetes/issues/1697) for more info.
When using dockerhub, regisrty syncer monitors a tag (example latest) and when the latest image is change i.e. the digest of the image is changed Syncer picks it up as a candidate deployment and deploys new version. 


### Run locally
```sh
    INSTANCENAME=ecrsync-photoapp-6b7d47c58f-xtdp6  cvmanager dr sync \
    --tag=env-usdev-api \
    --repo=<>.dkr.ecr.ap-southeast-2.amazonaws.com/nearmap/photo \
    --deployment=photoapp \
    --container=photoapp-container \
    --namespace=photo \
    --sync=1  \
    --k8s-config ~/.kube/config

```


### ECR Tagger Util
A tagging tool that integrates with CI side of things to manage tags on the ECR repositories.

#### Get Tag
```sh
    cvmanager dr tags get \
    --repo  nearmap/cvmanager  \
    --version <SHA>
```

#### Add Tag
```sh
    cvmanager dr tags remove \
    --repo  nearmap/cvmanager  \
    --tags env-audev-api,env-usdev-api \
    --version <SHA>
```

#### Remove Tag
```sh
    cvmanager dr tags remove \
    --repo  nearmap/cvmanager  \
    --tags env-audev-api,env-usdev-api
```


#### Supporting other docker registries
We plan to support other docker registries as well in future via cvmanager. 


## Building and running CVManager

### Build & Run
```sh
docker build -t nearmap/cvmanager .
docker run -ti  nearmap/cvmanager <command>
```

### Testing with docker-compose
```sh
 docker-compose down
 docker-compose rm -f
 docker-compose up --force-recreate --build --abort-on-container-exit
```


## Deploying CVManager to Kubernetes cluster
CVManager can be deployed using:

1. Kubectl: yaml specs for Kubenetes configuration is [here](kubectl/README.md)
2. Helm: Helm chart spec is [here](helm/cvmanager) and helm package is avaialble [here](https://raw.githubusercontent.com/nearmap/cvmanager/master/k8s/helm/cvmanager/cvmanager-0.1.0.tgz)

Please [see](k8s/README.md) for more info.




#### Reference links
- https://kccnceu18.sched.com/event/DquY/continuous-delivery-meets-custom-kubernetes-controller-a-declarative-configuration-approach-to-cicd-suneeta-mall-simon-cochrane-nearmap-intermediate-skill-level?iframe=no&w=100%&sidebar=yes&bg=no
