# What are these yaml files

The yaml files in this directory are resource files necessary to manually deploy an operator.
They are generated from the *generate-deploy* make target.

## Note ##

If you have changed the CRD definitions or any other config resources, you need to regenerate these yamls
by run the following command from the project root.

```
make generate-deploy
```

# How to use the yamls in this dir to deploy an operator

## To deploy an operator watching single namespace (operator's namespace)

Assuming the target namespace is current namespace (if not use kubectl's -n option to specify the target namespace)

1. Deploy all the crds
```
kubectl create -f ./crds
```

2. Deploy operator

You need to deploy all the yamls from this dir except *cluster_role.yaml* and *cluster_role_binding.yaml*:
```
kubectl create -f ./deploy/service_account.yaml
kubectl create -f ./deploy/role.yaml
kubectl create -f ./deploy/role_binding.yaml
kubectl create -f ./deploy/election_role.yaml
kubectl create -f ./deploy/election_role_binding.yaml
kubectl create -f ./deploy/operator.yaml
```

## To deploy an operator watching all namespace

The steps are similar to those for single namespace operators except that you replace the *role.yaml* and *role_binding.yaml* with *cluster_role.yaml* and *cluster_role_binding.yaml* respectively.

**Note**
Before deploy, you should edit operator.yaml and change the **WATCH_NAMESPACE** env var value to be empty string
and also change the subjects namespace to match your target namespace in cluster_role_binding.yaml
as illustrated in following

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: arkmq-org-broker-manager-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: arkmq-org-broker-operator
subjects:
- kind: ServiceAccount
  name: arkmq-org-broker-controller-manager
  namespace: <<This must match your operator's target namespace>>
```

After making the above changes deploy the operator as follows
(Assuming the target namespace is current namespace. You can use kubectl's -n option to specify otherwise)

```
kubectl create -f ./deploy/crds
kubectl create -f ./deploy/service_account.yaml
kubectl create -f ./deploy/cluster_role.yaml
kubectl create -f ./deploy/cluster_role_binding.yaml
kubectl create -f ./deploy/election_role.yaml
kubectl create -f ./deploy/election_role_binding.yaml
kubectl create -f ./deploy/operator.yaml
```
