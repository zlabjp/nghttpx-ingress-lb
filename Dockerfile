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

FROM debian:buster as build

COPY extra-mrbgem.patch static.patch 0001-nghttpx-Deal-with-the-case-when-h2-backend-is-retire.patch /

# Inspired by clean-install https://github.com/kubernetes/kubernetes/blob/73641d35c7622ada9910be6fb212d40755cc1f78/build/debian-base/clean-install
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git g++ make binutils autoconf automake autotools-dev libtool pkg-config \
        zlib1g-dev libev-dev libjemalloc-dev ruby-dev libc-ares-dev libssl-dev bison patch && \
    git clone --depth 1 -b v1.41.0 https://github.com/nghttp2/nghttp2.git && \
    cd nghttp2 && \
    patch -p1 < /extra-mrbgem.patch && \
    patch -p1 < /static.patch && \
    patch -p1 < /0001-nghttpx-Deal-with-the-case-when-h2-backend-is-retire.patch && \
    git submodule update --init && \
    autoreconf -i && \
    ./configure --disable-examples --disable-hpack-tools --disable-python-bindings --with-mruby --with-neverbleed && \
    make -j$(nproc) install-strip && \
    cd .. && \
    rm -rf nghttp2 && \
    apt-get -y purge git g++ make binutils autoconf automake autotools-dev libtool pkg-config \
        zlib1g-dev libev-dev libjemalloc-dev ruby-dev libc-ares-dev libssl-dev bison patch && \
    apt-get -y autoremove --purge && \
    rm -rf \
        /var/cache/debconf/* \
        /var/lib/apt/lists/* \
        /var/log/* \
        /tmp/* \
        /var/tmp/* && \
    rm /extra-mrbgem.patch /static.patch /0001-nghttpx-Deal-with-the-case-when-h2-backend-is-retire.patch

FROM gcr.io/distroless/cc-debian10@sha256:3b737b1c505bb093f53dd34401b0238d1fcfd22014e9b1fbba5e2a4c6f19de1f

COPY --from=build /usr/local/bin/nghttpx /usr/local/bin/
COPY image/var/log/nghttpx /var/log/nghttpx
COPY nghttpx-ingress-controller fetch-ocsp-response cat-ocsp-resp nghttpx.tmpl nghttpx-backend.tmpl default.tmpl /

WORKDIR /

CMD ["/nghttpx-ingress-controller"]
