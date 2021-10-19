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

FROM debian:11 as build

COPY patches/extra-mrbgem.patch /

# Inspired by clean-install https://github.com/kubernetes/kubernetes/blob/73641d35c7622ada9910be6fb212d40755cc1f78/build/debian-base/clean-install
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git clang make binutils autoconf automake autotools-dev libtool pkg-config \
        zlib1g-dev libev-dev libjemalloc-dev ruby-dev libc-ares-dev bison libelf-dev patch

RUN git clone --depth 1 -b openssl-3.0.0+quic https://github.com/quictls/openssl && \
    cd openssl && \
    ./config --openssldir=/etc/ssl && \
    make -j$(nproc) && \
    make install_sw && \
    cd .. && \
    rm -rf openssl

RUN git clone --depth 1 https://github.com/ngtcp2/nghttp3 && \
    cd nghttp3 && \
    autoreconf -i && \
    ./configure --enable-lib-only && \
    make -j$(nproc) && \
    make install-strip && \
    cd .. && \
    rm -rf nghttp3

RUN git clone --depth 1 https://github.com/ngtcp2/ngtcp2 && \
    cd ngtcp2 && \
    autoreconf -i && \
    ./configure --enable-lib-only \
        LIBTOOL_LDFLAGS="-static-libtool-libs" \
        OPENSSL_LIBS="-l:libssl.a -l:libcrypto.a -ldl -lpthread" \
        PKG_CONFIG_PATH="/usr/local/lib64/pkgconfig" && \
    make -j$(nproc) && \
    make install-strip && \
    cd .. && \
    rm -rf ngtcp2

RUN git clone --depth 1 -b v0.4.0 https://github.com/libbpf/libbpf && \
    cd libbpf && \
    PREFIX=/usr/local make -C src install && \
    cd .. && \
    rm -rf libbpf

RUN git clone --depth 1 -b v1.46.0 https://github.com/nghttp2/nghttp2.git && \
    cd nghttp2 && \
    patch -p1 < /extra-mrbgem.patch && \
    git submodule update --init && \
    autoreconf -i && \
    ./configure --disable-examples --disable-hpack-tools --disable-python-bindings --with-mruby --with-neverbleed \
        --enable-http3 --with-libbpf \
        CC=clang CXX=clang++ \
        LIBTOOL_LDFLAGS="-static-libtool-libs" \
        JEMALLOC_LIBS="-l:libjemalloc.a" \
        LIBEV_LIBS="-l:libev.a" \
        OPENSSL_LIBS="-l:libssl.a -l:libcrypto.a -ldl -pthread" \
        LIBCARES_LIBS="-l:libcares.a" \
        ZLIB_LIBS="-l:libz.a" \
        LIBBPF_LIBS="-L/usr/local/lib64 -l:libbpf.a -l:libelf.a" \
        PKG_CONFIG_PATH="/usr/local/lib64/pkgconfig" && \
    make -j$(nproc) install-strip && \
    cd .. && \
    rm -rf nghttp2

FROM gcr.io/distroless/cc-debian11:latest-amd64

COPY --from=build /usr/local/bin/nghttpx /usr/local/bin/
COPY --from=build /usr/local/lib/nghttp2/reuseport_kern.o \
    /usr/local/lib/nghttp2/
COPY image/var/log/nghttpx /var/log/nghttpx
COPY nghttpx-ingress-controller fetch-ocsp-response cat-ocsp-resp /

WORKDIR /

CMD ["/nghttpx-ingress-controller"]
