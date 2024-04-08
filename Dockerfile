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

FROM debian:12 as build

COPY patches/extra-mrbgem.patch patches/0001-nghttpx-Fix-QUIC-stateless-reset-stack-buffer-overfl.patch /

# Inspired by clean-install https://github.com/kubernetes/kubernetes/blob/73641d35c7622ada9910be6fb212d40755cc1f78/build/debian-base/clean-install
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        git clang gcc make binutils autoconf automake autotools-dev libtool pkg-config cmake cmake-data \
        zlib1g-dev libev-dev libjemalloc-dev ruby-dev libc-ares-dev bison libelf-dev patch libbrotli-dev

RUN git clone --depth 1 -b v1.23.0 https://github.com/aws/aws-lc && \
    cd aws-lc && \
    cmake -B build -DCMAKE_BUILD_TYPE=RelWithDebInfo -DDISABLE_GO=ON && \
    make -j$(nproc) -C build && \
    cmake --install build && \
    cd .. && \
    rm -rf aws-lc

RUN git clone --recursive --shallow-submodules --depth 1 -b v1.2.0 https://github.com/ngtcp2/nghttp3 && \
    cd nghttp3 && \
    autoreconf -i && \
    ./configure --enable-lib-only && \
    make -j$(nproc) && \
    make install-strip && \
    cd .. && \
    rm -rf nghttp3

RUN git clone --recursive --shallow-submodules --depth 1 -b v1.4.0 https://github.com/ngtcp2/ngtcp2 && \
    cd ngtcp2 && \
    autoreconf -i && \
    ./configure --enable-lib-only --with-boringssl \
        LIBTOOL_LDFLAGS="-static-libtool-libs" \
        BORINGSSL_LIBS="-l:libssl.a -l:libcrypto.a" \
        PKG_CONFIG_PATH="/usr/local/lib64/pkgconfig" && \
    make -j$(nproc) && \
    make install-strip && \
    cd .. && \
    rm -rf ngtcp2

RUN git clone --depth 1 -b v1.3.0 https://github.com/libbpf/libbpf && \
    cd libbpf && \
    PREFIX=/usr/local make -C src install && \
    cd .. && \
    rm -rf libbpf

RUN git clone --recursive --shallow-submodules --depth 1 -b v1.61.0 https://github.com/nghttp2/nghttp2.git && \
    cd nghttp2 && \
    patch -p1 < /extra-mrbgem.patch && \
    patch -p1 < /0001-nghttpx-Fix-QUIC-stateless-reset-stack-buffer-overfl.patch && \
    autoreconf -i && \
    ./configure --disable-examples --disable-hpack-tools --with-mruby \
        --enable-http3 --with-libbpf \
        --with-libbrotlienc --with-libbrotlidec \
        CC=clang CXX=clang++ \
        LDFLAGS="-static-libgcc -static-libstdc++" \
        LIBTOOL_LDFLAGS="-static-libtool-libs" \
        JEMALLOC_LIBS="-l:libjemalloc.a" \
        LIBEV_LIBS="-l:libev.a" \
        OPENSSL_LIBS="-l:libssl.a -l:libcrypto.a" \
        LIBCARES_LIBS="-l:libcares.a" \
        ZLIB_LIBS="-l:libz.a" \
        LIBBPF_LIBS="-L/usr/local/lib64 -l:libbpf.a -l:libelf.a" \
        LIBBROTLIENC_LIBS="-l:libbrotlienc.a -l:libbrotlicommon.a" \
        LIBBROTLIDEC_LIBS="-l:libbrotlidec.a -l:libbrotlicommon.a" \
        PKG_CONFIG_PATH="/usr/local/lib64/pkgconfig" && \
    make -j$(nproc) install-strip && \
    cd .. && \
    rm -rf nghttp2

FROM gcr.io/distroless/base-nossl-debian12:latest

COPY --from=build --link /usr/local/bin/nghttpx /usr/local/bin/
COPY --from=build --link /usr/local/lib/nghttp2/reuseport_kern.o \
    /usr/local/lib/nghttp2/
COPY --link image/var/log/nghttpx /var/log/nghttpx
COPY --link nghttpx-ingress-controller fetch-ocsp-response cat-ocsp-resp /

WORKDIR /

CMD ["/nghttpx-ingress-controller"]
