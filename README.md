# nghttpx Ingress Controller

This is an Ingress Controller which uses
[nghttpx](https://nghttp2.org/documentation/nghttpx.1.html) as L7 load
balancer.

nghttpx ingress controller was initially created based on
[nginx ingress controller](https://github.com/kubernetes/contrib/tree/master/ingress/controllers/nginx).

## Docker images

The official Docker images since v0.67.0 are available at
[GitHub Container Registry](https://github.com/zlabjp/nghttpx-ingress-lb/pkgs/container/nghttpx-ingress-controller).
The images for older releases can be found at [Docker
Hub](https://hub.docker.com/r/zlabjp/nghttpx-ingress-controller/).

## Requirements

- default backend [404-server](examples/default-backend.yaml)

Actually, any backend web server will suffice as long as it returns
some kind of error code for any requests.

If `--internal-default-backend` flag is given, the default backend
service is not necessary.  The controller configures nghttpx to act as
a default backend.

## Deploy the Ingress controller

Before the deploy of the Ingress controller we need a default backend:

```
$ kubectl create -f examples/default-backend.yaml
$ kubectl expose deployment default-http-backend --port=80 --target-port=8080 --name=default-http-backend
```

Load balancers are created via a Deployment or DaemonSet:

```
$ kubectl create -f examples/default/service-account.yaml
$ kubectl create -f examples/default/rc-default.yaml
```

## IngressClass

This controller supports IngressClass resource.  The default
IngressClass controller name is "zlab.co.jp/nghttpx".  It supports
`ingressclass.kubernetes.io/is-default-class` annotation.

This controller no longer supports the deprecated
`kubernetes.io/ingress.class` annotation.

The default behavior around IngressClass does not follow the standard
rule for a historical reason.  nghttpx ingress controller processes
Ingress resource which does not have .spec.ingressClassName specified.
It also interprets the default IngressClass in its own way.  If
Ingress resource does not have .spec.ingressClassName specified, but
the default IngressClass does not point to nghttpx ingress controller,
it does not process the resource.  The standard rule is that if
Ingress resource does not have .spec.ingressClassName, it should be
ignored, and only process the resource which is explicitly designated
to the controller via IngressClass.  `--require-ingress-class` flag
enforces this rule.  Obviously, it completely changes which resources
are processed by this controller.  You need to set
.spec.ingressClassName for all Ingress resources in your cluster.  And
create the default IngressClass resource to ensure that
Ingress.spec.ingressClassName is defaulted properly.

## networking.k8s.io/v1 Ingress

This controller only recognizes Service backend.  It ignores pathType
and behaves as if `ImplementationSpecific` is specified.  Hosts in
.spec.tls are also ignored.

## HTTP

First we need to deploy some application to publish. To keep this
simple we will use the [echoheaders app](https://github.com/kubernetes/contrib/blob/master/ingress/echoheaders/echo-app.yaml)
that just returns information about the HTTP request as output

```
kubectl create deployment echoheaders --image=registry.k8s.io/echoserver:1.10
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
NAME      HOSTS                     ADDRESS         PORTS   AGE
echomap   foo.bar.com,bar.baz.com   192.168.0.1     80      1m11s
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

You can secure an Ingress by specifying a secret that contains a TLS
private key and certificate. Currently the Ingress only supports a
single TLS port, 443, and assumes TLS termination. This controller
supports SNI. The TLS secret must contain keys named tls.crt and
tls.key that contain the certificate and private key to use for TLS:

```yaml
apiVersion: v1
data:
  tls.crt: <base64 encoded cert>
  tls.key: <base64 encoded key>
kind: Secret
metadata:
  name: testsecret
  namespace: default
type: kubernetes.io/tls
```

You can create this kind of secret using `kubectl create secret tls`
subcommand.

Referencing this secret in an Ingress will tell the Ingress controller
to secure the channel from the client to the load balancer using TLS:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: no-rules-map
spec:
  tls:
  - secretName: testsecret
  defaultBackend:
    service:
      name: s1
      port:
        number: 80
```

If TLS is configured for a service, and it is accessed via cleartext
HTTP, those requests are redirected to HTTPS URI.  If
`--default-tls-secret` flag is used, all cleartext HTTP requests are
redirected to https URI.  This behaviour is configurable using
[path-config](#ingresszlabcojppath-config-annotation) annotation.

## TLS OCSP stapling

By default, nghttpx performs OCSP request to OCSP responder for each
certificate.  This requires that the controller pod is allowed to make
outbound connections to the server.  If there are several Ingress
controllers, this method is not efficient since each controller
performs OCSP request.

With `--fetch-ocsp-resp-from-secret` flag, the controller fetches OCSP
response from TLS Secret described above.  Although we have to store
OCSP response to these Secrets in a separate step, and update
regularly, they are shared among all controllers, and therefore it is
efficient for large deployment.

Note that currently the controller has no facility to store and update
OCSP response to TLS Secret.  The controller just fetches OCSP
response from TLS Secret.

The key for OCSP response in TLS Secret is `tls.ocsp-resp` by default.
It can be changed by `--ocsp-resp-key` flag.  The value of OCSP
response in TLS Secret must be DER encoded.

## Sharing TLS ticket keys

By default, each nghttpx encrypts TLS ticket by its own key.  This
means that if there are several nghttpx ingress controller instances,
TLS session resumption might not work if the new connection goes to
the different instance.  With `--share-tls-ticket-key` flag, the
controller generates TLS ticket key in a Secret specified by
`--nghttpx-secret`, which is shared by all controllers.  This ensures
that all nghttpx instances use the same encryption key, which enables
stable TLS session resumption.

## HTTP/3 (Experimental)

In order to enable the experimental HTTP/3 feature, run the controller
with `--http3` flag.  The controller will create and maintain a
Secret, specified by `--nghttpx-secret` flag, which contains QUIC
keying materials in the same namespace as the controller Pod.  The
controller maintains the secret as a whole, and it should not be
altered by an external tool or user.  The keying materials are rotated
and new key is generated in the interval specified by
`--quic-secret-period` flag.  nghttpx listens on UDP port specified by
`--nghttpx-https-port` flag.

> [!WARNING]
>
> As of v0.66.0, Secret is integrated to the one specified by
> `--nghttpx-secret` flag, and `--quic-keying-materials-secret` flag
> has been removed.  The default value is also changed.  Previously,
> it is `nghttpx-quic-km` but now `nghttpx-km`.  To migrate from the
> previous release, before upgrading nghttpx-ingress-controller to
> v0.66.0, copy Secret `nghttpx-quic-km` to `nghttpx-km`, and upgrade
> nghttpx-ingress-controller.  The keying materials are now rotated
> and new key is generated in every 4 hours by default.  The new key
> is first placed at the end of the list.  In the next rotation, it is
> moved to the first, and is used for encryption.

HTTP/3 requires the extra capabilities to load eBPF program.  Add the
following capabilities to the nghttpx-ingress-controller container:

```yaml
apiVersion: apps/v1
kind: DaemonSet
...
spec:
  template:
    spec:
      containers:
      - image: zlabjp/nghttpx-ingress-controller:latest
        ...
        securityContext:
          capabilities:
            add:
            - SYS_ADMIN
            - SYS_RESOURCE
        ...
```

## PROXY protocol support - preserving ClientIP addresses

In case you are running nghttpx-ingress-lb behind a LoadBalancer you might
to preserve the ClientIP addresses accessing your Kubernetes cluster.

As an example we are using a deployment on a Kubernetes on AWS.

In order to use all nghttpx features, especially upstream HTTP/2 forwarding
and TLS SNI, the only way to deploy nghttpx-ingress-lb, is to use an
AWS ELB (Classic LoadBalancer) in TCP mode and let nghttpx do the
TLS-termination, because:

- AWS ELB does not handle HTTP/2 at all.
- AWS ALB does not handle upstream HTTP/2.

Therefore using an `X-Forward-For` header does not work, and you have to rely
on the PROXY-protocol feature.

### Enable PROXY-protocol on external LoadBalancer

You can enable the PROXY protocol manually on the external AWS
ELB(http://docs.aws.amazon.com/elasticloadbalancing/latest/classic/enable-proxy-protocol.html),
which forwards traffic to your nghttpx-ingress-lb, or you can let
Kubernetes handle this for your like:

```yaml
# Kubernetes LoadBalancer ELB configuration, which forwards traffic
# on ports 80 and 443 to an nghttpx-ingress-lb controller.
# Kubernetes enabled the PROXY protocol on the AWS ELB.
apiVersion: v1
kind: Service
metadata:
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-proxy-protocol: '*'
  name: nghttpx-ingress
  namespace: kube-system
spec:
  selector:
    k8s-app: nghttpx-ingress-lb
  type: LoadBalancer
  ports:
  - name: http-ingress
    port: 80
  - name: tls-ingress
    port: 443
```

### Enable PROXY-protocol on nghttpx-ingress-lb

Once the external LoadBalancer has PROXY-protocol enabled, you have to enable
the PROXY-protocol on nghttpx-ingress-lb as well by specifying an additional
launch-parameter of the nghttpx-ingress-lb, using `--proxy-proto=true`

See the [examples/proxyproto](examples/proxyproto) subdirectory for a working
deployment or use:

```sh
# Deploy nghttpx-ingress-lb behind LoadBalancer with PROXY protocol and RBAC enabled.
kubctl apply -f examples/proxyproto/
```

## Default backend

The default backend is used when the request does not match any given
rules.  The default backend must be set in command-line flag of
nghttpx Ingress controller unless `--internal-default-backend` flag is
given, see below.  It can be overridden by specifying
Ingress.Spec.Backend.  If multiple Ingress resources have
.Spec.Backend, one of them is used, but it is undefined which one is
used.  The default backend always does not require TLS.

If `--internal-default-backend` is used, the controller configures
nghttpx to act as a default backend.  In this case, the default
backend service is not necessary.

## Services without selectors

nghttpx supports [Services without
selectors](https://kubernetes.io/docs/concepts/services-networking/service/#services-without-selectors).

## Logs

The access, and error log of nghttpx are written to stdout, and stderr
respectively.  They can be configured using accesslog-file and
errorlog-file options respectively.  No log file rotation is
configured by default.

## Ingress status

By default, nghttpx Ingress controller periodically writes the
addresses of their Pods in all Ingress resource status.  If multiple
nghttpx Ingress controllers are running, the controller first gets all
Pods with the same labels of its own, and writes all addresses in
Ingress status.

If a Service is specified in `--publish-service` flag, external IPs,
and load balancer addresses in the specified Service are written into
Ingress resource instead.

## Additional configurations

nghttpx supports additional configurations via Ingress Annotations.

### `ingress.zlab.co.jp/backend-config` annotation

nghttpx-ingress-controller understands
`ingress.zlab.co.jp/backend-config` key in Ingress
`.metadata.annotations` to configure the particular backend.  Its
value is a serialized YAML or JSON dictionary.  The configuration is
done per service port
(`.spec.rules[*].http.paths[*].backend.service.port`).  The first key
under the root dictionary is the name of service name
(`.spec.rules[*].http.paths[*].backend.service.name`).  Its value is
the JSON dictionary, and its keys are service port
(`.spec.rules[*].http.paths[*].backend.service.port.name` or
`.spec.rules[*].http.paths[*].backend.service.port.number` if
`.port.name` is not specified).  The final value is the JSON
dictionary, and can contain the following key value pairs:

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

* `weight`: Specify the weight of the backend selection.  nghttpx
  ingress controller can aggregates multiple services under single
  host and path pattern.  The weight specifies how frequently this
  service is selected compared to the other services aggregated under
  the same pattern.  The service with weight 3 is 3 times more
  frequently used than the one with weight 1.  Using this settings,
  one can send more/less traffic to a particular service.  This is
  useful, for example, if one wants to send 80% of traffic to Service
  A, while remaining 20% traffic to Service B.  The value must be [1,
  256], inclusive.

The following example specifies HTTP/2 as backend connection for
service "greeter", and service port "50051":

```yaml
apiVersion: networking.k8s.io/v1
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
        pathType: ImplementationSpecific
        backend:
          service:
            name: greeter
            port:
              number: 50051
```

Or in YAML:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: greeter
  annotations:
    ingress.zlab.co.jp/backend-config: |
      greeter:
        50051:
          proto: h2
spec:
  rules:
  - http:
      paths:
      - path: /helloworld.Greeter/
        pathType: ImplementationSpecific
        backend:
          service:
            name: greeter
            port:
              number: 50051
```

The controller also understands
`ingress.zlab.co.jp/default-backend-config` annotation.  It serves
default values for missing values in backend-config per field basis in
the same Ingress resource.  It can contain single dictionary which
contains the same key/value pairs.  It is useful if same set of
backend-config is required for lots of services.

For example, if a pair of service, and port has the backend-config
like so:

```json
{"sni": "www.example.com"}
```

And the default-backend-config looks like so:

```json
{"proto": "h2", "sni": "example.com"}
```

The final backend-config becomes like so:

```json
{"proto": "h2", "sni": "www.example.com"}
```

A values which specified explicitly in an individual backend-config
always takes precedence.

Note that Ingress allows regular expression in
`.spec.rules[*].http.paths[*].path`, but nghttpx does not support it.

### `ingress.zlab.co.jp/path-config` annotation

nghttpx-ingress-controller understands
`ingress.zlab.co.jp/path-config` key in Ingress
`.metadata.annotations` to allow additional configuration per host and
path pattern.  Its value is a serialized YAML or JSON dictionary.  The
configuration is done per host and path pattern.  The key under the
root dictionary is the concatenation of host and path.  For example,
if host is "www.example.com" and path is "/foo", its key is
"www.example.com/foo".  For convenience, if "www.example.com" is
specified as a key, it is normalized as "www.example.com/".  Its value
is the dictionary and can contain the following key value pairs:

* `mruby`: Specify mruby script which is invoked when the given
  pattern is selected.  For mruby script, see [nghttpx manual
  page](https://nghttp2.org/documentation/nghttpx.1.html#mruby-scripting)

* `affinity`: Specify session affinity method.  Specifying `ip`
  enables client IP based session affinity.  Specifying `cookie`
  enables cookie-based session affinity.  Specifying `none` or
  omitting this key disables session affinity.

  If `cookie` is specified, additional configuration is required.  See
  `affinityCookieName`, `affinityCookiePath`, and
  `affinityCookieSecure` fields.

* `affinityCookieName`: Specify a name of cookie to use.  This is
  required field if `cookie` is set in `affinity` field.

* `affinityCookiePath`: Specify a path of cookie path.  This is
  optional, and if not set, cookie path is not set.

* `affinityCookieSecure`: Specify whether Secure attribute of cookie
  is added, or not.  Omitting this field, specifying empty string, or
  specifying "auto" sets Secure attribute if client connection is TLS
  encrypted.  If "yes" is specified, Secure attribute is always added.
  If "no" is specified, Secure attribute is always omitted.

* `affinityCookieStickiness`: Specify the stickiness of session
  cookie.  If `loose` is given, which is default, the affinity might
  break if an existing backend server is removed, or new backend
  server is added.  If `strict` is given, if the designated backend
  server is removed, the request is forwarded to a new server as if it
  is a new request.  However, adding new backend server does not cause
  breakage.

* `readTimeout`: Specify read timeout.  If specified, it overrides
  global backend read timeout set by
  [--backend-read-timeout](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-backend-read-timeout).
  You can use string representation of time used in Golang (e.g., 5s,
  5m)

* `writeTimeout`: Specify write timeout.  If specified, it overrides
  global backend write timeout set by
  [--backend-write-timeout](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-backend-write-timeout).
  You can use string representation of time used in Golang (e.g., 5s,
  5m)

* `redirectIfNotTLS`: Specify whether cleartext HTTP request is
  redirected to HTTPS if TLS is configured.  This defaults to true.

* `doNotForward`: Do not forward a request to a backend server.  It
  assumes that a response is generated by mruby script.
  `.spec.rules[*].http.paths[*].backend` is a required field, so a
  placeholder service must be given (it never be accessed).  Note that
  `.spec.rules[*].http.paths[*].backend.resource` does not work as a
  placeholder.

Here is an example to rewrite request path to "/foo" from "/pub/foo" using mruby:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: greeter
  annotations:
    ingress.zlab.co.jp/path-config: |
      www.example.com/pub/foo:
        readTimeout: 5m
        mruby: |
          class App
            def on_req(env)
              env.req.path = "/foo"
            end
          end
          App.new
spec:
  rules:
  - host: www.example.com
    http:
      paths:
      - path: /pub/foo
        pathType: ImplementationSpecific
        backend:
          service
            name: bar
            port:
              number: 80
```

The controller also understands
`ingress.zlab.co.jp/default-path-config` annotation.  It serves
default values for missing values in path-config per field basis in
the same Ingress resource.  It can contain single dictionary which
contains the same key/value pairs.  It is useful if same configuration
is shared by lots of patterns.

A values which specified explicitly in an individual path-config
always takes precedence.

## Custom nghttpx configuration

Using a ConfigMap it is possible to customize the defaults in nghttpx.
The content of configuration is specified under `nghttpx-conf` key.
All nghttpx options can be used to customize behavior of nghttpx.  See
[FILES](https://nghttp2.org/documentation/nghttpx.1.html#files)
section of nghttpx(1) manual page for the syntax of configuration
file.

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

nghttpx historically strips an incoming X-Forwarded-Proto header
field, and adds its own one.  To change this behaviour, use the
combination of
[no-add-x-forwarded-proto](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--no-add-x-forwarded-proto)
and
[no-strip-incoming-x-forwarded-proto](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--no-strip-incoming-x-forwarded-proto).
For example, in order to retain the incoming X-Forwarded-Proto header
field, add `no-strip-incoming-x-forwarded-proto=yes`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nghttpx-ingress-lb
data:
  nghttpx-conf: |
    no-strip-incoming-x-forwarded-proto=yes
```

nghttpx ingress controller, by default, overrides the following default configuration:

- [workers](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-n):
  set the number of cores that nghttpx uses.

User can override `workers` using ConfigMap.

Since
[mruby-file](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx--mruby-file)
option takes a path to mruby script file, user has to include mruby
script to the image or mount the external volume.  In order to make it
easier to specify mruby script, user can write mruby script under
`nghttpx-mruby-file-content` key, like so:

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
Read [MRUBY SCRIPTING](https://nghttp2.org/documentation/nghttpx.1.html#mruby-scripting)
section of nghttpx(1) manual page about mruby API.

## MRUBY Scripting

In addition to the basic mrbgems included by mruby, this Ingress
controller adds the following mrbgems for convenience:

- [mattn/mruby-onig-regexp](https://github.com/mattn/mruby-onig-regexp):
  This adds the regular expression support.

## Troubleshooting

TBD

### Debug

Using the `--v` flag it is possible to increase the level of logging.
In particular:
- `--v=2` shows details using `diff` about the changes in the configuration in nghttpx

```
I0323 04:39:16.552830       8 utils.go:90] nghttpx configuration diff /etc/nghttpx/nghttpx.conf
--- current
+++ new
@@ -1,7 +1,41 @@
-# A very simple nghttpx configuration file that forces nghttpx to start.
+accesslog-file=/dev/stdout
+include=/etc/nghttpx/nghttpx-backend.conf
```

- `--v=3` shows details about the service, Ingress rule, endpoint changes and it dumps the nghttpx configuration in JSON format

## Limitations

- When no TLS is configured, nghttpx still listens on port 443 for cleartext HTTP.
- TLS configuration is not bound to the specific service.  In general,
  all proxied services are accessible via TLS.
- `.spec.rules[*].http.paths[*].pathType` is ignored and it is treated
  as if `ImplementationSpecific` is specified.  Consult [nghttpx
  manual](https://nghttp2.org/documentation/nghttpx.1.html#cmdoption-nghttpx-b)
  to know the path matching rules.

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

```
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
```

This repository contains the code which has the following license
notice:

```
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
```
