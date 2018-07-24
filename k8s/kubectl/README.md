# Deploy kcd using kubectl

Modify optional settings such as history or rollback options by editing [kcd config](kcd.yaml) specifying appropriate preference flag for kcd run arg:
```
            - "--history=false"
            - "--rollback=false"
```

## Deploy kcd and all required resources
```sh
 kubectl apply -f kcd.yaml
 kubectl apply -f rbac-role.yaml
```

*Delete*
```sh
 kubectl delete -f kcd.yaml
 kubectl  get crd kcd 
```

