# Copyright 2015 The Kubernetes Authors. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Copyright 2016, Z Lab Corporation. All rights reserved.
#
# For the full copyright and license information, please view the LICENSE
# file that was distributed with this source code.

FROM ubuntu:16.04

RUN apt-get update && apt-get install -y git g++ make binutils autoconf automake autotools-dev libtool pkg-config \
        zlib1g-dev libssl-dev libev-dev libjemalloc-dev ruby-dev bison \
        zlib1g libssl1.0.0 libev4 libjemalloc1 \
        diffutils ca-certificates psmisc \
        --no-install-recommends && \
    git clone -b v1.14.0 https://github.com/nghttp2/nghttp2.git && \
    cd nghttp2 && \
    git submodule update --init && autoreconf -i && \
    ./configure --disable-examples --disable-hpack-tools --disable-python-bindings --with-mruby --with-neverbleed && \
    make install-strip && \
    cd .. && \
    rm -rf nghttp2 && \
    apt-get -y purge git g++ make binutils autoconf automake autotools-dev libtool pkg-config \
        zlib1g-dev libssl-dev libev-dev libjemalloc-dev ruby-dev bison && \
    apt-get -y autoremove --purge && \
    apt-get clean

RUN mkdir -p /var/log/nghttpx
RUN mkdir -p /etc/nghttpx
RUN touch /etc/nghttpx/nghttpx-backend.conf

COPY nghttpx-ingress-controller /
COPY nghttpx.tmpl /
COPY nghttpx-backend.tmpl /
COPY default.conf /etc/nghttpx/nghttpx.conf
COPY nghttpx-start.sh /
COPY nghttpx-reload.sh /

WORKDIR /

CMD ["/nghttpx-ingress-controller"]
