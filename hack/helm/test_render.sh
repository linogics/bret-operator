#!/usr/bin/env bash
# Render the chart with provider flags and watchNamespaces and assert the
# RBAC output.
set -uo pipefail

chart=${1:-charts/bret-operator}

fail() {
	echo "FAIL: $*" >&2

	exit 1
}

render() {
	helm template bret "$chart" "$@" || fail "helm template $*"
}

# docs PATTERN...: print each YAML doc from stdin that matches every
# grep -E pattern. Capture the output instead of piping it into
# `grep -q`, which can SIGPIPE the loop and fail under pipefail.
docs() {
	local doc='' line

	while IFS= read -r line; do
		if [[ $line == '---' ]]; then
			match_doc "$doc" "$@"
			doc=''

			continue
		fi

		doc+=$line$'\n'
	done

	match_doc "$doc" "$@"
}

match_doc() {
	local doc=$1 pattern
	shift

	for pattern in "$@"; do
		grep -qE -- "$pattern" <<<"$doc" || return 0
	done

	printf '%s\n' "$doc"
}

# cluster_role: print the manager ClusterRole from stdin.
cluster_role() {
	docs '^kind: ClusterRole$' '-manager$'
}

helm dependency build "$chart" >/dev/null || fail 'helm dependency build'

# Defaults with velero off: kopiur only, no velero or volsync rules.
out=$(render --set providers.velero.enabled=false) || exit

cr=$(cluster_role <<<"$out")

grep -q 'namespaces' <<<"$cr" \
	&& fail 'namespaces rule rendered with velero off'
grep -q 'velero.io' <<<"$out" \
	&& fail 'velero.io rendered with providers.velero.enabled=false'
grep -q 'kopiur.home-operations.com' <<<"$out" \
	|| fail 'kopiur rules missing by default'
grep -q 'volsync.backube' <<<"$out" \
	&& fail 'volsync rules rendered by default'

# Provider flags switch their rules on and off.
out=$(render \
	--set providers.velero.enabled=true \
	--set providers.volsync.enabled=true \
	--set providers.kopiur.enabled=false) || exit

grep -q 'velero.io' <<<"$out" || fail 'velero rules missing when enabled'
grep -q 'volsync.backube' <<<"$out" \
	|| fail 'volsync rules missing when enabled'
grep -q 'kopiur.home-operations.com' <<<"$out" \
	&& fail 'kopiur rules rendered when disabled'
grep -q 'name: PROVIDERS' <<<"$out" || fail 'PROVIDERS env missing'

# Every provider off.
out=$(render --set providers.kopiur.enabled=false) || exit

grep -A1 'name: PROVIDERS' <<<"$out" \
	| grep -q 'value: "none"' \
	|| fail 'PROVIDERS is not none with every provider off'

# Custom velero namespace.
out=$(render \
	--set providers.velero.enabled=true \
	--set providers.velero.namespace=velero-system) || exit

grep -A1 'name: VELERO_NAMESPACE' <<<"$out" \
	| grep -q 'value: "velero-system"' \
	|| fail 'VELERO_NAMESPACE env missing'

# Velero on with watched namespaces.
out=$(render \
	--set providers.velero.enabled=true \
	--set 'watchNamespaces={a,b}') || exit

# Leader-election Role plus one Role per watched namespace plus the
# velero Role.
n=$(grep -c '^kind: Role$' <<<"$out")
((n == 4)) \
	|| fail "expected 4 Roles (leader election, 2 watched, velero), got $n"

grep -q 'name: WATCH_NAMESPACES' <<<"$out" \
	|| fail 'WATCH_NAMESPACES env missing'

# R14: velero-only checks need namespaces rights that stay cluster-wide,
# and bret rules move to the Roles.
cr=$(cluster_role <<<"$out")

grep -q 'persistentvolumeclaims' <<<"$cr" \
	|| fail 'pods/pvcs left the ClusterRole with velero on'
grep -q 'namespaces' <<<"$cr" \
	|| fail 'namespaces rights left the ClusterRole'
grep -q 'bret.linogics.io' <<<"$cr" \
	&& fail 'bret.linogics.io rules in ClusterRole with watchNamespaces set'

for ns in a b; do
	role=$(docs '^kind: Role$' "namespace: $ns\$" 'bret.linogics.io' \
		<<<"$out")

	[[ -n $role ]] || fail "no Role in watched namespace $ns"
done

# F9: velero off, watched namespaces: pods/pvcs move into the Roles.
out=$(render --set 'watchNamespaces={a,b}') || exit
cr=$(cluster_role <<<"$out")

grep -q 'persistentvolumeclaims' <<<"$cr" \
	&& fail 'pods/pvcs in ClusterRole with velero off, ns watched'

for ns in a b; do
	role=$(docs '^kind: Role$' "namespace: $ns\$" 'persistentvolumeclaims' \
		<<<"$out")

	[[ -n $role ]] || fail "no pods/pvc rights in Role of $ns"
done

# F9: the ClusterRole has no update verb for pods/pvcs.
out=$(render) || exit

cr=$(cluster_role <<<"$out")

grep -A12 'persistentvolumeclaims' <<<"$cr" \
	| grep -q -- '- update' \
	&& fail 'update verb on pods/pvcs'

# Velero rights live in the velero namespace, not in watched Roles.
out=$(render \
	--set providers.velero.enabled=true \
	--set 'watchNamespaces={app}') || exit

role=$(docs '^kind: Role$' 'namespace: velero' 'velero.io' <<<"$out")
binding=$(docs 'kind: RoleBinding' 'namespace: velero' <<<"$out")
leak=$(docs '^kind: Role$' 'namespace: app' 'velero.io' <<<"$out")

[[ -n $role ]] || fail 'velero Role missing in velero namespace'
[[ -n $binding ]] || fail 'velero RoleBinding missing in velero namespace'
[[ -n $leak ]] && fail 'velero.io leaked into watched Role'

# Secrets rights follow volsync.
out=$(render) || exit
cr=$(cluster_role <<<"$out")

grep -q 'secrets' <<<"$cr" && fail 'secrets rule rendered with volsync off'

out=$(render --set providers.volsync.enabled=true) || exit
cr=$(cluster_role <<<"$out")

grep -A3 'secrets' <<<"$cr" \
	| grep -q -- '- get' \
	|| fail 'secrets get missing with volsync on'

# Volsync with watched namespaces: secrets move into the Roles.
out=$(render \
	--set providers.volsync.enabled=true \
	--set 'watchNamespaces={a,b}') || exit
cr=$(cluster_role <<<"$out")

grep -q 'secrets' <<<"$cr" \
	&& fail 'secrets in ClusterRole with watchNamespaces set'

for ns in a b; do
	role=$(docs '^kind: Role$' "namespace: $ns\$" 'secrets' <<<"$out")

	[[ -n $role ]] || fail "no secrets get in Role of $ns"
done

grep -q 'configmaps' <<<"$cr" \
	&& fail 'velero copy rights rendered with velero off'

# Velero copies check Secrets into the unwatched scratch namespace, so the
# copy rights stay cluster-wide.
out=$(render \
	--set providers.velero.enabled=true \
	--set 'watchNamespaces={a}') || exit
cr=$(cluster_role <<<"$out")

grep -A8 'configmaps' <<<"$cr" \
	| grep -q -- '- create' \
	|| fail 'velero copy rights missing from ClusterRole'

echo OK
