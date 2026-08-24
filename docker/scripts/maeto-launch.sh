#!/bin/sh
bin="$1"
shift

if [ -z "$MAETO_DLV_LISTEN" ]; then
	exec "$bin" "$@"
fi

if [ ! -x /usr/local/bin/dlv ]; then
	echo "MAETO_DLV_LISTEN is set but dlv is not in this image; rebuild with DEBUG=1" >&2
	exec "$bin" "$@"
fi

echo "starting $bin under delve on $MAETO_DLV_LISTEN" >&2

exec /usr/local/bin/dlv \
	--listen="$MAETO_DLV_LISTEN" \
	--headless=true \
	--api-version=2 \
	--accept-multiclient \
	--continue \
	--log \
	exec "$bin" -- "$@"
