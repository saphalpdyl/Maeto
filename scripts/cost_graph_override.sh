#! /bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dir=$root/tools/debug/control-plane
py=$dir/.venv/bin/python

if [ ! -x "$py" ]; then
  python3 -m venv --clear "$dir/.venv" >&2
  "$py" -m pip install -q -r "$dir/requirements.txt" >&2
fi

exec "$py" "$dir/main.py" "$@"
