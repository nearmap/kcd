# Deploy CVManager using kubectl

Modify optional settings such as history or rollback options by editing [cvmanager config](cvmanager.yaml) specifying appropriate preference flag for cvmanager run arg:
```
            - "--history=false"
            - "--rollback=false"
```

## Deploy CVManager and all required resources
```sh
 kubectl apply -f cvmanager.yaml
```

*Delete*
```sh
 kubectl delete -f cvmanager.yaml
 kubectl  get crd cv 
```

