[![CircleCI](https://circleci.com/gh/nearmap/cvmanager.svg?style=svg&circle-token=e635659d5d8190eb041cc92149262a5b75470fcd)](https://circleci.com/gh/nearmap/cvmanager)[![Code Quality](https://goreportcard.au-api.nearmap.com/badge/github.com/nearmap/cvmanager)](https://goreportcard.au-api.nearmapdev.com/report/github.com/nearmap/cvmanager)

# cvmanager
Container Version Manager (cvmanager) is a continous integration (CI) and continous delivery (CD) tool designed for Kubernetes cluster/services. Fundamentally, cvmanager is a custom Kubernetes controller to achieve a declarative configuration approach to continuous deployment. 

Deployments that requires CI/CD, can declare [ContainerVersion](k8s/cv-crd.yaml) resource. [CVManager](k8s/Backend.yaml), ContainerVersion controller starts monitoring for any new changes that should be rolled-out. If so, using the rollout strategy specified in this deployment, the rollout of new version is carried out.

CVManager assumes ECR as the container registry. Supporting other registeries is T2D.

The tool has 3 main parts:
- CV Manager
- ECR Syncer
- ECR Tagger

Docker images are on [docker.io](https://hub.docker.com/r/nearmap/cvmanager/)

## CV Manager
ContainerVersion controller that manages ContainerVersion resources.

### Run locally
```
 REGION=us-east-1 AWS_DEFAULT_REGION=us-east-1 VERSION=3ae8d1b85862921af30de6d0461d2e73086f5961  cvmanager run \
    --k8s-config ~/.kube/config 
```

### ECR Sync service
The polling service that frequently check on ECR to see if new version should be rolled out for a given deployment/container.

### Run locally
```
INSTANCENAME=<ecrsync-tilesapp-podname> cvmanager ecr-sync \
    --tag=dev \
    --ecr=nearmap/tiles \
    --deployment=tilesapp \
    --container=tilesapp-container \
    --namespace=default \
    --sync=1 \
    --k8s-config ~/.kube/config
```


### ECR Tagger Util
A tagging tool that integrates with CI side of things to manage tags on the ECR repositories.

#### Get Tag
```sh
    cvmanager ecr-tags get \
    --ecr  nearmap/cvmanager  \
    --version <SHA>
```

#### Add Tag
```sh
    cvmanager ecr-tags remove \
    --ecr  nearmap/cvmanager  \
    --tags env-audev-api,env-usdev-api \
    --version <SHA>
```

#### Remove Tag
```sh
    cvmanager ecr-tags remove \
    --ecr  nearmap/cvmanager  \
    --tags env-audev-api,env-usdev-api
```


## Docker 

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


## Supporting other docker registries
We plan to support other docker registries as well in future via cvmanager. Dockerhub was first consideration however given [tags on dockerhub can not yet be removed via API](https://github.com/docker/hub-feedback/issues/68) this feature is still into consideration. 

1. https://github.com/kubernetes/kubernetes/issues/33664
2. https://github.com/kubernetes/kubernetes/issues/11348
3. https://github.com/kubernetes/kubernetes/issues/1697


#### Reference links
- https://kccnceu18.sched.com/event/DquY/continuous-delivery-meets-custom-kubernetes-controller-a-declarative-configuration-approach-to-cicd-suneeta-mall-simon-cochrane-nearmap-intermediate-skill-level?iframe=no&w=100%&sidebar=yes&bg=no
