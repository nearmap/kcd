Details [here](https://blog.openshift.com/kubernetes-deep-dive-code-generation-customresources/)
[client](client) and *generated.*.go* in [apis](apis) are generated using [code-generator](k8s.io/code-generator) k8s project: 

To generate:

```sh
 CODEGEN_PKG=../../../k8s.io/code-generator  bash -xe codegen/update-codegen.sh
```


To verify:

```sh
 CODEGEN_PKG=../../../k8s.io/code-generator  bash -xe codegen/verify-codegen.sh
```


- [bug](https://github.com/kubernetes/code-generator/issues/21)


