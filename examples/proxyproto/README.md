Creates an nghttpx-ingress-lb DaemonSet behind an AWS ELB.

- ClientIP addresses are preserved by enabling the PROXY-protocol.
- Use `X-Forwarded-For` header in your downstream backend to access the
  ClientIP.
- RBAC is enabled.
- Hostnetwork=true (needed when you run a CNI-plugin [Flannel,Weave,Calico]).  Remove this param if you do not need it.
