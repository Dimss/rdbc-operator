## Redis Enterprise DB Creation Operator
RDBC - K8S operator allowing to manage Redis DBs in K8S native way by CRDs and CRs. 

## Deployment
1. Deploy CRD: `oc apply -f deploy/crds/rdbc_v1alpha1_rdbc_crd.yaml`
2. Patch the `all-in-one.yaml` file and set correct NS. Since RDBC is a Cluster Scope Operator, you'll have to configure the `namespace` for `ClusterRoleBinding->Subject`
   Example:
   ```bash
      kind: ClusterRoleBinding
      apiVersion: rbac.authorization.k8s.io/v1
      metadata:
        name: rdbc-operator
      subjects:
      - kind: ServiceAccount
        name: rdbc-operator
        namespace: __REPLACE_WIHT_ACTUAL_NS_TO_WHERE_THE_OPERATOR_GONNA_BE_DEPLOYED__ 
      roleRef:
        kind: ClusterRole
        name: rdbc-operator
        apiGroup: rbac.authorization.k8s.io
    ``` 
2. Deploy Operator: `oc apply -f deploy/all-in-one.yaml`
3. Check Operator pod logs `oc logs -f operator-pod`   

# Create DBs
To create a new DB apply following CR `oc apply -f deploy/crds/rdbc_v1alpha1_rdbc_cr.yaml`
```bash
apiVersion: rdbc.cnative/v1alpha1
kind: Rdbc
metadata:
  name: my-app-db-request-1
  namespace: default
spec:
  # Name of the redis DB
  name: "my-app-db2"
  # Password for the redis DB
  password: "1234"
  # Size in Megabytes for redis DB
  size: 100
```

#### For local debugging  - useful commands
`sudo ssh -L 443:127.0.0.1:443 -p 2222 root@ocp-local`