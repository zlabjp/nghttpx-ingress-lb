#!/bin/bash
set -e

REPOS="\
api \
apiextensions-apiserver \
apimachinery \
apiserver \
cli-runtime \
cloud-provider \
cluster-bootstrap \
code-generator \
component-base \
client-go \
cri-api \
csi-translation-lib \
kube-aggregator \
kube-controller-manager \
kube-proxy \
kube-scheduler \
kubelet \
legacy-cloud-providers \
metrics \
sample-apiserver \
"

for repo in $REPOS; do
    git clone --depth 1 -b kubernetes-1.15.0 https://github.com/kubernetes/$repo list-k8s-dep-temp
    cd list-k8s-dep-temp
    tstamp=`git log -1 --pretty=format:%ct | python -c "import sys,datetime; sys.stdout.write(datetime.datetime.utcfromtimestamp(int(sys.stdin.readline())).strftime(\"%Y%m%d%H%M%S\"))"`
    h=`git log -1 --pretty=format:%H|head -c12`
    echo "        k8s.io/$repo => k8s.io/$repo v0.0.0-$tstamp-$h"
    cd ..
    rm -rf list-k8s-dep-temp
done
