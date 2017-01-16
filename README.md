# nghttpx Ingress Controller

This is a nghttpx Ingress controller that uses
[ConfigMap](https://github.com/kubernetes/kubernetes/blob/master/docs/proposals/configmap.md)
to store the nghttpx configuration. See [Ingress controller
documentation](../README.md) for details on how it works.

nghttpx ingress controller is created based on
[nginx ingress controller](https://github.com/kubernetes/contrib/tree/master/ingress/controllers/nginx).

## What it provides?

- Ingress controller
- nghttpx >= 1.10
- TLS support
- custom nghttpx configuration using [ConfigMap](https://github.com/kubernetes/kubernetes/blob/master/docs/proposals/configmap.md)


## Requirements
- default backend [404-server](https://github.com/kubernetes/contrib/tree/master/404-server)


## Dry running the Ingress controller

TODO This does not work

Before deploying the controller to production you might want to run it outside the cluster and observe it.

```console
$ make controller
$ mkdir /etc/nghttpx-tls
$ ./nghttpx-ingress-controller --running-in-cluster=false --default-backend-service=kube-system/default-http-backend
```


## Deploy the Ingress controller

First create a default backend:
```
$ kubectl create -f examples/default-backend.yaml
$ kubectl expose rc default-http-backend --port=80 --target-port=8080 --name=default-http-backend
```

Loadbalancers are created via a ReplicationController or Daemonset:

```
$ kubectl create -f examples/default/service-account.yaml
$ kubectl create -f examples/default/rc-default.yaml
```

## HTTP

First we need to deploy some application to publish. To keep this simple we will use the [echoheaders app](https://github.com/kubernetes/contrib/blob/master/ingress/echoheaders/echo-app.yaml) that just returns information about the http request as output
```
kubectl run echoheaders --image=gcr.io/google_containers/echoserver:1.4 --replicas=1 --port=8080
```

Now we expose the same application in two different services (so we can create different Ingress rules)
```
kubectl expose deployment echoheaders --port=80 --target-port=8080 --name=echoheaders-x
kubectl expose deployment echoheaders --port=80 --target-port=8080 --name=echoheaders-y
```

Next we create a couple of Ingress rules
```
kubectl create -f examples/ingress.yaml
```

we check that ingress rules are defined:
```
$ kubectl get ing
NAME      RULE          BACKEND   ADDRESS
echomap   -
          foo.bar.com
          /foo          echoheaders-x:80
          bar.baz.com
          /bar          echoheaders-y:80
          /foo          echoheaders-x:80
```

Before the deploy of the Ingress controller we need a default backend [404-server](https://github.com/kubernetes/contrib/tree/master/404-server)
```
kubectl create -f examples/default-backend.yaml
kubectl expose rc default-http-backend --port=80 --target-port=8080 --name=default-http-backend
```

Check nghttpx it is running with the defined Ingress rules:

```
$ LBIP=$(kubectl get node `kubectl get po -l name=nghttpx-ingress-lb --template '{{range .items}}{{.spec.nodeName}}{{end}}'` --template '{{range $i, $n := .status.addresses}}{{if eq $n.type "ExternalIP"}}{{$n.address}}{{end}}{{end}}')
$ curl $LBIP/foo -H 'Host: foo.bar.com'
```

## TLS

You can secure an Ingress by specifying a secret that contains a TLS private key and certificate. Currently the Ingress only supports a single TLS port, 443, and assumes TLS termination. This controller supports SNI. The TLS secret must contain keys named tls.crt and tls.key that contain the certificate and private key to use for TLS, eg:

```yaml
apiVersion: v1
data:
  tls.crt: base64 encoded cert
  tls.key: base64 encoded key
kind: Secret
metadata:
  name: testsecret
  namespace: default
type: Opaque
```

Referencing this secret in an Ingress will tell the Ingress controller to secure the channel from the client to the loadbalancer using TLS:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: no-rules-map
spec:
  tls:
    secretName: testsecret
  backend:
    serviceName: s1
    servicePort: 80
```

Check the [example](examples/tls/README.md)

## Logs

The access and error log of nghttpx are written to
/var/log/nghttpx/access.log and /var/log/nghttpx/error.log
respectively.  They can be configured using accesslog-file and
errorlog-file options respectively.  No log file rotation is
configured by default.

## Additional backend connection configuration

nghttpx supports additional backend connection configuration via
Ingress Annotation.

nghttpx-ingress-controller understands
`ingress.zlab.co.jp/backend-config` key in Ingress
`.metadata.annotations`.  Its value is a serialized JSON dictionary.
The configuration is done per service port
(`.spec.rules[*].http.paths[*].backend.servicePort`).  The first key
under the root dictionary is the name of service name
(`.spec.rules[*].http.paths[*].backend.serviceName`).  Its value is
the JSON dictionary, and its keys are servie port
(`.spec.rules[*].http.paths[*].backend.servicePort`).  The final value
is the JSON dictionary, and can contain the following key value pairs:

* `proto`: Specify the application protocol used for this service
  port.  The value is of type string, and it should be either `h2`, or
  `http/1.1`.  Use `h2` to use HTTP/2 for backend connection.  This is
  optional, and defaults to "http/1.1".

* `tls`: Specify whether or not TLS is used for this service port.
  This is optional, and defaults to `false`.

* `sni`: Specify SNI hostname for TLS connection.  This is used to
  validate server certificate.

* `dns`: Specify whether backend host name should be resolved
  dynamically.

* `affinity`: Specify session affinity method.  Specifying `ip`
  enables client IP based session affinity.  Specifying `none` or
  omitting this key disables session affinity.

The following example specifies HTTP/2 as backend connection for
service "greeter", and service port "50051":

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: greeter
  annotations:
    ingress.zlab.co.jp/backend-config: '{"greeter": {"50051": {"proto": "h2"}}}'
spec:
  rules:
  - http:
      paths:
      - path: /helloworld.Greeter/
        backend:
          serviceName: greeter
          servicePort: 50051
```

Note that Ingress allows regular expression in
`.spec.rules[*].http.paths[*].path`, but nghttpx does not support it.

## Custom nghttpx configuration

Using a ConfigMap it is possible to customize the defaults in nghttpx.
All nghttpx options can be used to customize behavior of nghttpx.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nghttpx-ingress-lb
data:
  nghttpx-conf: |
    log-level=INFO
```

nghttpx ingress controller, by default, overrides the following default configuration:

- `workers`: set to the number of cores that the nghttpx ingress
  controller runs.

User can override `workers` using ConfigMap.

## Troubleshooting

TBD

### Debug

Using the flag `--v=XX` it is possible to increase the level of logging.
In particular:
- `--v=2` shows details using `diff` about the changes in the configuration in nghttpx

```
I1226 09:31:32.305044       1 utils.go:91] nghttpx configuration diff a//etc/nghttpx/nghttpx-backend.conf b//etc/nghttpx/nghttpx-backend.conf
I1226 09:31:32.305078       1 utils.go:92] --- /tmp/132090794	 2016-12-26 09:31:32.000000000 +0000
+++ /tmp/476970241	    2016-12-26 09:31:32.000000000 +0000
@@ -0,0 +1,3 @@
+# kube-system/default-http-backend,8080;/
+backend=10.2.50.3,8080;/;proto=http/1.1
+
I1226 09:31:32.305093       1 command.go:78] change in configuration detected. Reloading...
```

- `--v=3` shows details about the service, Ingress rule, endpoint changes and it dumps the nghttpx configuration in JSON format

## Limitations

- When no TLS is configured, ingress controller still listen on port 443 for cleartext HTTP.
- TLS configuration is not bound to the specific service.  In general,
  all proxied services are accessible via both TLS and cleartext HTTP.
- Ingress allows regular expression in
  `.spec.rules[*].http.paths[*].path`, but nghttpx does not support it.

## Building from source

Build nghttpx-ingress-controller binary:

```
$ make controller
```

Build and push docker images:

```
$ make push
```

# LICENSE

The MIT License (MIT)

Copyright (c) 2016  Z Lab Corporation

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

This repository contains the code which has the following license
notice:

Copyright 2015 The Kubernetes Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
