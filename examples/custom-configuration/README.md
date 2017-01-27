Write nghttpx's configuration in a same format that nghttpx accepts
with --conf option.

For instance, if we want to change the log-level to "INFO", do like
so:

```
$ cat conf.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nghttpx-ingress-lb
data:
  nghttpx-conf: |
    log-level=INFO
```

```
$ kubectl create -f conf.yaml
```

Pass `--nghttpx-configmap=default/nghttpx-ingress-lb` to
nghttpx-ingress-controller to tell the lb ConfigMap name.

If the ConfigMap it is updated, nghttpx will be reloaded with the new
configuration.
