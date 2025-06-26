#!/usr/bin/env bash

# Return early if no proxy is set
if [ -z "{{ .Proxy }}" ]; then
    return 0
fi

# Set proxy environment variables
export HTTP_PROXY={{ .Proxy }}
export HTTPS_PROXY={{ .Proxy }}
export NO_PROXY=localhost 