#!/usr/bin/env bash
# Render the chart with provider flags and watchNamespaces and assert the RBAC output.
set -euo pipefail
CHART=${1:-charts/bret-operator}
fail() { echo "FAIL: $*" >&2; exit 1; }

out=$(helm template bret "$CHART" --set providers.velero.enabled=false)
awk 'BEGIN{RS="---\n"} /kind: ClusterRole\n/ && /-manager\n/' <<<"$out" | grep -q 'namespaces' && fail "namespaces rule rendered with velero off"
[ "$(grep -c 'velero.io' <<<"$out")" = 0 ] || fail "velero.io rendered with providers.velero.enabled=false"
grep -q 'kopiur.home-operations.com' <<<"$out" || fail "kopiur rules missing by default"
grep -q 'volsync.backube' <<<"$out" && fail "volsync rules rendered by default"

out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set providers.volsync.enabled=true --set providers.kopiur.enabled=false)
grep -q 'velero.io' <<<"$out" || fail "velero rules missing when enabled"
grep -q 'volsync.backube' <<<"$out" || fail "volsync rules missing when enabled"
grep -q 'kopiur.home-operations.com' <<<"$out" && fail "kopiur rules rendered when disabled"
grep -q 'name: PROVIDERS' <<<"$out" || fail "PROVIDERS env missing"

out=$(helm template bret "$CHART" --set providers.kopiur.enabled=false)
grep -A1 'name: PROVIDERS' <<<"$out" | grep -q 'value: "none"' || fail "PROVIDERS is not none with every provider off"

out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set providers.velero.namespace=velero-system)
grep -A1 'name: VELERO_NAMESPACE' <<<"$out" | grep -q 'value: "velero-system"' || fail "VELERO_NAMESPACE env missing"

out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set 'watchNamespaces={a,b}')
n=$(grep -c '^kind: Role$' <<<"$out" || true)
# leader-election Role plus one Role per watched namespace
[ "$n" = 4 ] || fail "expected 4 Roles (leader election, 2 watched, velero), got $n"
grep -q 'name: WATCH_NAMESPACES' <<<"$out" || fail "WATCH_NAMESPACES env missing"
# R14: velero-only checks need namespaces rights that stay cluster-wide, and bret rules move to the Roles
cr=$(awk 'BEGIN{RS="---\n"} /kind: ClusterRole\n/ && /-manager\n/' <<<"$out")
grep -q 'persistentvolumeclaims' <<<"$cr" || fail "pods/pvcs left the ClusterRole with velero on"
grep -q 'namespaces' <<<"$cr" || fail "namespaces rights left the ClusterRole"
grep -q 'bret.linogics.io' <<<"$cr" && fail "bret.linogics.io rules in ClusterRole with watchNamespaces set"
for ns in a b; do
  awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: '$ns'\n/ && /bret.linogics.io/' <<<"$out" | grep -q . || fail "no Role in watched namespace $ns"
done
# F9: velero off, watched namespaces: pods/pvcs move into the Roles, and the ClusterRole has no update verb for them
out=$(helm template bret "$CHART" --set 'watchNamespaces={a,b}')
cr=$(awk 'BEGIN{RS="---\n"} /kind: ClusterRole\n/ && /-manager\n/' <<<"$out")
grep -q 'persistentvolumeclaims' <<<"$cr" && fail "pods/pvcs in ClusterRole with velero off and watchNamespaces set"
for ns in a b; do
  awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: '$ns'\n/ && /persistentvolumeclaims/' <<<"$out" | grep -q . || fail "no pods/pvc rights in Role of $ns"
done
out=$(helm template bret "$CHART")
awk 'BEGIN{RS="---\n"} /kind: ClusterRole\n/ && /-manager\n/' <<<"$out" | grep -A12 'persistentvolumeclaims' | grep -q -- '- update' && fail "update verb on pods/pvcs"

out=$(helm template bret "$CHART" --set providers.velero.enabled=true --set 'watchNamespaces={app}')
awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: velero/ && /velero.io/' <<<"$out" | grep -q . || fail "velero Role missing in velero namespace"
awk 'BEGIN{RS="---\n"} /kind: RoleBinding/ && /namespace: velero/' <<<"$out" | grep -q . || fail "velero RoleBinding missing in velero namespace"
awk 'BEGIN{RS="---\n"} /kind: Role\n/ && /namespace: app/ && /velero.io/' <<<"$out" | grep -q . && fail "velero.io leaked into watched Role"
echo OK
