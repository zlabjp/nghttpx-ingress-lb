#!/bin/sh

# Copyright 2016, Z Lab Corporation. All rights reserved.
#
# For the full copyright and license information, please view the LICENSE
# file that was distributed with this source code.

if [ -f /run/nghttpx.pid ]; then
    PID=`cat /run/nghttpx.pid`

    kill -USR2 "$PID" || exit 1

    invoked=0
    for t in 0.1 0.2 0.4 0.8 1.6 3.2 6.4; do
	NEWPID=`cat /run/nghttpx.pid`
	if [ "$PID" != "$NEWPID" ]; then
	    invoked=1
	    break
	fi

	sleep $t
    done

    [ $invoked -eq 1 ] || exit 1

    kill -QUIT "$PID"
fi
