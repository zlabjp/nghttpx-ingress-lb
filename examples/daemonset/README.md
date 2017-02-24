Since Ingress controller uses hostPort, it is better to run as
DaemonSet.  The file `as-daemonset.yaml` contains an example

```
kubectl create -f as-daemonset.yaml
```