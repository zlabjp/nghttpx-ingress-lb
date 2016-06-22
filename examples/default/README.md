
Create the Ingress controller
```
kubectl create -f rc-default.yaml
```

To test if evertyhing is working correctly:

`curl -v http://<node IP address>/foo -H 'Host: foo.bar.com'`

You should see an output similar to
```
*   Trying 172.17.4.99...
* Connected to 172.17.4.99 (172.17.4.99) port 80 (#0)
> GET /foo HTTP/1.1
> host: foo.bar.com
> User-Agent: curl/7.47.0
> Accept: */*
>
< HTTP/1.1 200 OK
< Date: Thu, 23 Jun 2016 01:20:25 GMT
< Content-Type: text/plain
< Transfer-Encoding: chunked
< Server: nghttpx nghttp2/1.12.0-DEV
< Via: 1.1 nghttpx
<
CLIENT VALUES:
client_address=10.2.53.4
command=GET
real path=/foo
query=nil
request_version=1.1
request_uri=http://foo.bar.com:8080/foo

SERVER VALUES:
server_version=nginx: 1.10.0 - lua: 10001

HEADERS RECEIVED:
accept=*/*
host=foo.bar.com
user-agent=curl/7.47.0
via=1.1 nghttpx
x-forwarded-proto=http
BODY:
* Connection #0 to host 172.17.4.99 left intact
-no body in request-
```

If we try to get a non exising route like `/foobar` we should see
```
$ curl -v 172.17.4.99/foobar -H 'Host: foo.bar.com'
*   Trying 172.17.4.99...
* Connected to 172.17.4.99 (172.17.4.99) port 80 (#0)
> GET /foobar HTTP/1.1
> host: foo.bar.com
> User-Agent: curl/7.47.0
> Accept: */*
>
< HTTP/1.1 404 Not Found
< Date: Thu, 23 Jun 2016 01:23:28 GMT
< Content-Length: 21
< Content-Type: text/plain; charset=utf-8
< Server: nghttpx nghttp2/1.12.0-DEV
< Via: 1.1 nghttpx
<
* Connection #0 to host 172.17.4.99 left intact
default backend - 404
```

(this test checked that the default backend is properly working)
