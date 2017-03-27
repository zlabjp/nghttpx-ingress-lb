# nghttpx Ingress Controller

This is a nghttpx Ingress controller that uses
[ConfigMap](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/configmap.md)
to store the nghttpx configuration. See [Ingress controller
documentation](../README.md) for details on how it works.

nghttpx ingress controller is created based on
[nginx ingress controller](https://github.com/kubernetes/contrib/tree/master/ingress/controllers/nginx).

## Docker images

The official Docker images are available at [Docker Hub](https://hub.docker.com/r/zlabjp/nghttpx-ingress-controller/).

## Requirements
- default backend [404-server](https://github.com/kubernetes/contrib/tree/master/404-server)


## Deploy the Ingress controller

Before the deploy of the Ingress controller we need a default backend [404-server](https://github.com/kubernetes/contrib/tree/master/404-server)
```
$ kubectl create -f examples/default-backend.yaml
$ kubectl expose deployment default-http-backend --port=80 --target-port=8080 --name=default-http-backend
```

Loadbalancers are created via a ReplicationController or Daemonset:

```
$ kubectl create -f examples/default/service-account.yaml
$ kubectl create -f examples/default/rc-default.yaml
```

## Ingress class

This controller supports "kubernetes.io/ingress.class" Ingress
annotation.  By default, the controller processes "nghttpx" class.  It
also processes the Ingress object which has no Ingress class
annotation, or its value is empty.

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

Check nghttpx it is running with the defined Ingress rules:

```
$ LBIP=$(kubectl get node `kubectl get po -l name=nghttpx-ingress-lb --namespace=kube-system --template '{{range .items}}{{.spec.nodeName}}{{end}}'` --template '{{range $i, $n := .status.addresses}}{{if eq $n.type "ExternalIP"}}{{$n.address}}{{end}}{{end}}')
$ curl $LBIP/foo -H 'Host: foo.bar.com'
```

The above command might not work properly.  In that case, check out
Ingress resource's .Status.LoadBalancer.Ingress field.  nghttpx
Ingress controller periodically (30 - 60 seconds) writes its IP
address there.

## TLS

You can secure an Ingress by specifying a secret that contains a TLS private key and certificate. Currently the Ingress only supports a single TLS port, 443, and assumes TLS termination. This controller supports SNI. The TLS secret must contain keys named tls.crt and tls.key that contain the certificate and private key to use for TLS, eg:

```yaml
apiVersion: v1
data:
  tls.crt: <base64 encoded cert>
  tls.key: <base64 encoded key>
kind: Secret
metadata:
  name: testsecret
  namespace: default
type: Opaque
```

You can create this kind of secret using `kubectl create secret tls`
subcommand.

Referencing this secret in an Ingress will tell the Ingress controller to secure the channel from the client to the loadbalancer using TLS:

```yaml
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: no-rules-map
spec:
  tls:
  - secretName: testsecret
  backend:
    serviceName: s1
    servicePort: 80
```

If TLS is configured for a service, and it is accessed via cleartext
HTTP, those requests are redirected to https URI.  If
--default-tls-secret flag is used, all cleartext HTTP requests are
redirected to https URI.

## Default backend

The default backend is used when the request does not match any given
rules.  The default backend must be set in command-line flag of
nghttpx Ingress controller.  It can be overridden by specifying
Ingress.Spec.Backend.  If multiple Ingress resources have
.Spec.Backend, one of them is used, but it is undefined which one is
used.  The default backend always does not require TLS.

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
The content of configuration is specified under `nghttpx-conf` key.
All nghttpx options can be used to customize behavior of nghttpx.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nghttpx-ingress-lb
data:
  nghttpx-conf: |
    log-level=INFO
    accesslog-file=/dev/null
```

nghttpx ingress controller, by default, overrides the following default configuration:

- `workers`: set to the number of cores that the nghttpx ingress
  controller runs.

User can override `workers` using ConfigMap.

Since `mruby-file` option takes a path to mruby script file, user has
to include mruby script to the image or mount the external volume.  In
order to make it easier to specify mruby script, user can write mruby
script under `nghttpx-mruby-file-content` key, like so:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nghttpx-ingress-lb
data:
  nghttpx-mruby-file-content: |
    class App
      def on_req(env)
        env.req.path = "/apps#{env.req.path}"
      end
    end

    App.new
```

The controller saves the content, and mruby-file option which refers
to the saved file is added to the configuration.

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
  all proxied services are accessible via TLS.
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
Copyright (c) 2017  nghttpx Ingress controller contributors

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
