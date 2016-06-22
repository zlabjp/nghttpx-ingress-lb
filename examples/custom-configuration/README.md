The next command shows the defaults:
```
$ ./nghttpx-ingress-controller --dump-nghttpx-configuration
Example of ConfigMap to customize nghttpx configuration:
data:
  backend-http2-connection-window-bits: "30"
  backend-http2-window-bits: "16"
  backend-read-timeout: 1m
  backend-write-timeout: 30s
  ciphers: ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA256
  frontend-http2-connection-window-bits: "16"
  frontend-http2-window-bits: "16"
  log-level: NOTICE
  no-ocsp: "true"
  tls-proto-list: TLSv1.2
  workers: "1"
metadata:
  creationTimestamp: null
  name: custom-name
  namespace: a-valid-namespace
```

For instance, if we want to change the log-level to "INFO", do like
so:

```
$ cat nghttpx-ingress-lb-conf.yaml
apiVersion: v1
data:
  log-level: "INFO"
kind: ConfigMap
metadata:
  name: nghttpx-ingress-lb

```

```
$ kubectl create -f nghttpx-ingress-lb-conf.yaml
```

Pass `--nghttpx-configmap=default/nghttpx-ingress-lb-conf` to
nghttpx-ingress-controller to tell the lb Configmap name.

If the Configmap it is updated, nghttpx will be reloaded with the new
configuration.
