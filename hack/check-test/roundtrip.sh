#!/usr/bin/env bash
# Full check test for one app: seed source data, back it up with Kopiur,
# restore into scratch PVCs, run the RestoreCheck check against them.
#
# Run it from the repo root.
#
# usage: hack/check-test/roundtrip.sh APP [SEED_YAML] [CHECK_YAML]
#   defaults: checks/APP/seed.yaml, checks/APP/restorecheck.yaml
# Seed contract: SEED_YAML creates one PVC per volume key (see render.py --list)
#   and a Pod named "seed" that writes data and the canary, then exits 0.
# env: KEEP=1 keeps the namespace bpt-APP for debugging.
#      TIMEOUT per wait in seconds (default 900).

app=${1:-}
seed=${2:-checks/$app/seed.yaml}
bp=${3:-checks/$app/restorecheck.yaml}
ns=bpt-$app
timeout_s=${TIMEOUT:-900}
render=hack/check-test/render.py
# Qualified, because the operator has a Restore kind too.
kopiur=kopiur.home-operations.com

if [[ -z $app || ! -f $seed || ! -f $bp ]]; then
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 2
fi

log() {
	echo "[$app] $*"
}

wait_jsonpath() {
	# args: namespace kind name jsonpath want
	local ns=$1 kind=$2 name=$3 path=$4 want=$5 end got=""

	end=$((SECONDS + timeout_s))

	while ((SECONDS < end)); do
		got=$(kubectl -n "$ns" get "$kind" "$name" -o jsonpath="$path" \
			2>/dev/null)

		if [[ $got == "$want" ]]; then
			return 0
		fi

		sleep 5
	done

	log "TIMEOUT waiting $kind/$name $path == $want (last: '$got')"
	return 1
}

cleanup() {
	if [[ -z ${KEEP:-} ]]; then
		kubectl delete ns "$ns" --ignore-not-found --wait=false >/dev/null
	fi
}

manifests=hack/check-test/manifests

# Applies a static manifest with a JSON merge patch. The patch sets the values of one volume.
apply_patched() {
	local file=$1 patch=$2

	kubectl patch --local -f "$manifests/$file" --type=merge -p "$patch" -o yaml \
		| kubectl -n "$ns" apply -f - >/dev/null
}

kopiur_repo() {
	kubectl -n "$ns" create secret generic kopiur-repo-creds \
		--from-literal=AWS_ACCESS_KEY_ID=AKIDSPIKE \
		--from-literal=AWS_SECRET_ACCESS_KEY=SECRETSPIKE12345 \
		--from-literal=KOPIA_PASSWORD=spike-kopia-pass >/dev/null || return 1

	apply_patched repository.yaml \
		"{\"spec\":{\"backend\":{\"s3\":{\"prefix\":\"bpt/$app/\"}}}}"
}

kopiur_policy() {
	# args: volume key, mover patch (may be null)
	apply_patched snapshotpolicy.yaml \
		"{\"metadata\":{\"name\":\"$1\"},\"spec\":{\"sources\":[{\"pvc\":{\"name\":\"$1\"}}],\"mover\":$2}}"
}

kopiur_snapshot() {
	# args: volume key
	apply_patched snapshot.yaml \
		"{\"metadata\":{\"name\":\"$1-1\"},\"spec\":{\"policyRef\":{\"name\":\"$1\"}}}"
}

kopiur_restore() {
	# args: volume key, size, mover patch (may be null)
	apply_patched restore.yaml \
		"{\"metadata\":{\"name\":\"r-$1\"},\"spec\":{\"source\":{\"snapshotRef\":{\"name\":\"$1-1\",\"namespace\":\"$ns\"}},\"mover\":$3}}" \
		|| return 1

	apply_patched restored-pvc.yaml \
		"{\"metadata\":{\"name\":\"r-$1\"},\"spec\":{\"resources\":{\"requests\":{\"storage\":\"$2\"}},\"dataSourceRef\":{\"name\":\"r-$1\"}}}"
}

mover_patch() {
	# args: uid, kind (policy or restore). Prints null when uid is empty, which leaves the mover unset.
	local uid=$1 kind=$2 sc

	if [[ -z $uid ]]; then
		echo null

		return 0
	fi

	sc="{\"runAsUser\":$uid,\"runAsGroup\":$uid,\"runAsNonRoot\":true}"

	if [[ $kind == restore ]]; then
		echo "{\"securityContext\":$sc,\"podSecurityContext\":{\"fsGroup\":$uid}}"

		return 0
	fi

	echo "{\"securityContext\":$sc}"
}

main() {
	local keys=() claims=() k uid size rc

	# Start from an empty namespace. A seed cannot initdb into an old data dir.
	if kubectl get ns "$ns" >/dev/null 2>&1; then
		kubectl delete ns "$ns" --wait=true --timeout=300s >/dev/null
	fi

	kubectl create ns "$ns" >/dev/null
	trap cleanup EXIT

	mapfile -t keys < <(python3 "$render" "$bp" --list)

	for k in "${keys[@]}"; do
		claims+=("$k=r-$k")
	done

	# 1. Repository and one SnapshotPolicy per volume.
	kopiur_repo || return 1

	for k in "${keys[@]}"; do
		uid=$(python3 "$render" "$bp" --uid "$k")
		kopiur_policy "$k" "$(mover_patch "$uid" policy)" || return 1
	done

	# 2. Seed the source data.
	log "seeding from $seed"
	kubectl -n "$ns" apply -f "$seed" >/dev/null

	if ! wait_jsonpath "$ns" pod seed '{.status.phase}' Succeeded; then
		kubectl -n "$ns" logs seed --all-containers --tail=80

		return 1
	fi

	kubectl -n "$ns" delete pod seed --ignore-not-found >/dev/null

	# 3. Back up each volume, restore it into a scratch PVC of the same size.
	for k in "${keys[@]}"; do
		size=$(kubectl -n "$ns" get pvc "$k" \
			-o jsonpath='{.spec.resources.requests.storage}')

		kopiur_snapshot "$k" || return 1
		wait_jsonpath "$ns" "snapshot.$kopiur" "$k-1" '{.status.phase}' Succeeded \
			|| return 1

		uid=$(python3 "$render" "$bp" --uid "$k")
		kopiur_restore "$k" "$size" "$(mover_patch "$uid" restore)" \
			|| return 1
		wait_jsonpath "$ns" "restore.$kopiur" "r-$k" '{.status.phase}' Completed \
			|| return 1
		wait_jsonpath "$ns" pvc "r-$k" '{.status.phase}' Bound || return 1
	done

	# 4. Run the check against the restored PVCs.
	log "running check $bp"
	hack/check-test/check.sh "$bp" "$ns" "check-${app//[^a-z0-9-]/-}" \
		"${claims[@]}"
	rc=$?

	log "result exit=$rc (0 = PASS)"

	return "$rc"
}

main
