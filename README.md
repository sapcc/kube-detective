# Kubernetes Network Detective

This tool is used to validate the network setup of a Kubernetes cluster. It
goes beyond the basic e2e tests and verifies each node of a cluster. This
allows to find even the most arcane or temporary network problems.

## Test Bed

Before any testing the following test bed is being created:

  * Ephemeral Namespace
  * Two pods per node. One with `hostNetwork` mode enabled, one without.
  * For each pod a service is created. A unique external IP is assigned to
      each.

## Test Scenarios

To test connectivty the tool will call a `kubectl exec wget http://$IP:$PORT`.
It tests the following scenarios.

  * Connectivity from Pod to Pod
  * Connectivity from Pod to ClusterIP to Pod
  * Connectivity from Pod to ExternalIP to Pod

It tests all possible permutations. This is not feasable for large clusters...
Only `schedulable` nodes are taken into account.

## Running

Default load order for `.kubeconfig` applies. If you have a working `kubectl`
it will just work

```
detective -externalCIDR 10.44.11.32/27
```

Additional logging can be enabled by setting `--v=2` or `--v=3`.

