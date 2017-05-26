#!/bin/bash
set -e

ocspresp=${1/%.crt/.ocsp-resp}

cat $ocspresp
