#!/bin/sh

# Copyright 2016, Z Lab Corporation. All rights reserved.
#
# For the full copyright and license information, please view the LICENSE
# file that was distributed with this source code.

[ -f /run/nghttpx.pid ] && rm -f /run/nghttpx.pid

/usr/local/bin/nghttpx || exit 1

while [ ! -f /run/nghttpx.pid ]; do
    sleep 1
done
