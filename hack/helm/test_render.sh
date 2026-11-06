#!/usr/bin/env bash
# Render the chart with provider flags and watchNamespaces and assert the RBAC output.
set -euo pipefail
CHART=${1:-charts/bret-operator}
fail() { echo "FAIL: $*" >&2; exit 1; }

out=$(helm template bret "$CHART" --set providers.velero.enabled=false)
[ "$(grep -c 'velero.io' <<<"$out")" = 0 ] || fail "velero.io rendered with providers.velero.enabled=false"
grep -q 'kopiur.home-operations.com' <<<"$out" || fail "kopiur rules missing by default"
grep -q 'volsync.backube' <<<"$out" && fail "volsync rules rendered by default"

out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set providers.volsync.enabled=true --set providers.kopiur.enabled=false)
grep -q 'velero.io' <<<"$out" || fail "velero rules missing when enabled"
grep -q 'volsync.backube' <<<"$out" || fail "volsync rules missing when enabled"
grep -q 'kopiur.home-operations.com' <<<"$out" && fail "kopiur rules rendered when disabled"
grep -q 'name: PROVIDERS' <<<"$out" || fail "PROVIDERS env missing"

out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set 'watchNamespaces={a,b}')
n=$(grep -c '^kind: Role$' <<<"$out" || true)
# leader-election Role plus one Role per watched namespace
[ "$n" = 4 ] || fail "expected 4 Roles (leader election, 2 watched, velero), got $n"
grep -q 'name: WATCH_NAMESPACES' <<<"$out" || fail "WATCH_NAMESPACES env missing"
# R14: velero-only checks need namespaces rights that stay cluster-wide, and bret rules move to the Roles
cr=$(awk 'BEGIN{RS="---\n"} /kind: ClusterRole\n/ && /-manager\n/' <<<"$out")
grep -q 'persistentvolumeclaims' <<<"$cr" || fail "pods/pvcs left the ClusterRole"
grep -q 'namespaces' <<<"$cr" || fail "namespaces rights left the ClusterRole"
grep -q 'bret.linogics.io' <<<"$cr" && fail "bret.linogics.io rules in ClusterRole with watchNamespaces set"
for ns in a b; do
  awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: '$ns'\n/ && /bret.linogics.io/' <<<"$out" | grep -q . || fail "no Role in watched namespace $ns"
done
out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set 'watchNamespaces={app}')
awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: velero/ && /velero.io/' <<<"$out" | grep -q . || fail "velero Role missing in velero namespace"
awk 'BEGIN{RS="---\n"} /kind: RoleBinding/ && /namespace: velero/' <<<"$out" | grep -q . || fail "velero RoleBinding missing in velero namespace"
awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: app/ && /velero.io/' <<<"$out" | grep -q . && fail "velero.io leaked into watched Role"
echo OK
