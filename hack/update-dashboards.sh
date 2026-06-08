#!/usr/bin/env bash
# Refetch a vendored Grafana dashboard from grafana.com.
#
# Usage: ./hack/update-dashboards.sh <nvidia|amd> [--revision N]
#
# Without --revision, downloads the latest revision and prints which one
# was picked. With --revision, pins to that exact revision.
#
# After running, bump the dashboard JSON's top-level "version" field by
# 1 so Grafana's sidecar reloads it from the ConfigMap.

set -euo pipefail

usage() {
  echo "Usage: $0 <nvidia|amd> [--revision N]" >&2
  exit 2
}

case "${1:-}" in
  nvidia) id=12239; out="charts/sims-monitoring/dashboards/nvidia-dcgm.json" ;;
  amd)    id=23715; out="charts/sims-monitoring/dashboards/amd-gpu.json" ;;
  ""|-h|--help) usage ;;
  *) echo "unknown vendor: $1" >&2; usage ;;
esac
shift

revision=""
while [ $# -gt 0 ]; do
  case "$1" in
    --revision) revision="$2"; shift 2 ;;
    *) echo "unknown flag: $1" >&2; usage ;;
  esac
done

if [ -z "$revision" ]; then
  echo "looking up latest revision for grafana dashboard $id" >&2
  revision=$(curl -fsSL "https://grafana.com/api/dashboards/$id" | jq -r .revision)
  echo "latest revision: $revision" >&2
fi

url="https://grafana.com/api/dashboards/$id/revisions/$revision/download"
echo "fetching $url -> $out" >&2
curl -fsSL "$url" -o "$out"

if ! jq -e . "$out" >/dev/null; then
  echo "fetched file is not valid JSON: $out" >&2
  exit 1
fi

echo
echo "wrote $out (revision $revision)"
echo "reminder: bump the JSON's top-level \"version\" field before committing"
echo "          so Grafana's sidecar reloads the dashboard."
