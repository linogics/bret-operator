#!/usr/bin/env bash
# Render the chart with provider flags and watchNamespaces and assert the RBAC output.
set -euo pipefail
CHART=${1:-charts/bret-operator}
fail() { echo "FAIL: $*" >&2; exit 1; }

out=$(helm template bret "$CHART" --set providers.velero=false)
[ "$(grep -c 'velero.io' <<<"$out")" = 0 ] || fail "velero.io rendered with providers.velero=false"
grep -q 'kopiur.home-operations.com' <<<"$out" || fail "kopiur rules missing by default"
grep -q 'volsync.backube' <<<"$out" && fail "volsync rules rendered by default"

out=$(helm template bret "$CHART" --set providers.velero=true --set providers.volsync=true --set providers.kopiur=false)
grep -q 'velero.io' <<<"$out" || fail "velero rules missing when enabled"
grep -q 'volsync.backube' <<<"$out" || fail "volsync rules missing when enabled"
grep -q 'kopiur.home-operations.com' <<<"$out" && fail "kopiur rules rendered when disabled"
grep -q 'name: PROVIDERS' <<<"$out" || fail "PROVIDERS env missing"

out=$(helm template bret "$CHART" --set providers.velero=true --set 'watchNamespaces={a,b}')
n=$(grep -c '^kind: Role$' <<<"$out" || true)
# leader-election Role plus one Role per watched namespace
[ "$n" = 3 ] || fail "expected 3 Roles (leader election + 2 watched), got $n"
grep -q 'name: WATCH_NAMESPACES' <<<"$out" || fail "WATCH_NAMESPACES env missing"
# R14: pods, PVCs and namespaces stay in the ClusterRole
awk '/^kind: ClusterRole$/{f=1} /^---/{f=0} f' <<<"$out" | grep -q 'persistentvolumeclaims' || fail "pods/pvcs left the ClusterRole"
echo OK
