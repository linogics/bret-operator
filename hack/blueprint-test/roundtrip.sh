#!/usr/bin/env bash
# Full blueprint test for one app: seed source data, back it up with Kopiur,
# restore into scratch PVCs, run the RestoreBlueprint check against them.
#
# usage: roundtrip.sh APP [SEED_YAML] [BLUEPRINT_YAML]
#   defaults: seeds/APP.yaml, ../../blueprints/APP.yaml
# Seed contract: SEED_YAML creates one PVC per volume key (see render.py --list)
#   and a Pod named "seed" that writes data and the canary, then exits 0.
# env: KEEP=1 keeps the namespace bpt-APP for debugging.
#      TIMEOUT per wait in seconds (default 900).

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)
app=$1
seed=${2:-$here/seeds/$app.yaml}
bp=${3:-$repo_root/blueprints/$app.yaml}
ns="bpt-$app"
timeout_s=${TIMEOUT:-900}

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

apply_yaml() {
	kubectl -n "$ns" apply -f - >/dev/null
}

kopiur_repo() {
	cat <<-YAML
		apiVersion: v1
		kind: Secret
		metadata: {name: kopiur-repo-creds}
		type: Opaque
		stringData:
		  AWS_ACCESS_KEY_ID: "AKIDSPIKE"
		  AWS_SECRET_ACCESS_KEY: "SECRETSPIKE12345"
		  KOPIA_PASSWORD: "spike-kopia-pass"
		---
		apiVersion: kopiur.home-operations.com/v1alpha1
		kind: Repository
		metadata: {name: primary}
		spec:
		  backend:
		    s3:
		      bucket: kopiur
		      prefix: bpt/$app/
		      endpoint: versitygw.storage.svc.cluster.local:7070
		      region: us-east-1
		      tls: {disableTls: true}
		      auth: {secretRef: {name: kopiur-repo-creds}}
		  encryption:
		    passwordSecretRef: {name: kopiur-repo-creds, key: KOPIA_PASSWORD}
		  create: {enabled: true}
	YAML
}

kopiur_policy() {
	# args: volume key, mover block (may be empty)
	cat <<-YAML
		apiVersion: kopiur.home-operations.com/v1alpha1
		kind: SnapshotPolicy
		metadata: {name: $1}
		spec:
		  repository: {kind: Repository, name: primary}
		  sources:
		    - pvc: {name: $1}
		  copyMethod: Snapshot
		  volumeSnapshotClassName: csi-hostpath-snapclass
		  retention: {keepDaily: 1}
		$2
	YAML
}

kopiur_snapshot() {
	# args: volume key
	cat <<-YAML
		apiVersion: kopiur.home-operations.com/v1alpha1
		kind: Snapshot
		metadata: {name: $1-1}
		spec: {policyRef: {name: $1}, deletionPolicy: Retain}
	YAML
}

kopiur_restore() {
	# args: volume key, size, mover block (may be empty)
	cat <<-YAML
		apiVersion: kopiur.home-operations.com/v1alpha1
		kind: Restore
		metadata: {name: r-$1}
		spec:
		  source: {snapshotRef: {name: $1-1, namespace: $ns}}
		  target: {populator: {}}
		$3
		  policy: {onMissingSnapshot: Fail, waitTimeout: 5m}
		---
		apiVersion: v1
		kind: PersistentVolumeClaim
		metadata: {name: r-$1}
		spec:
		  storageClassName: csi-hostpath-sc
		  accessModes: ["ReadWriteOnce"]
		  resources: {requests: {storage: $2}}
		  dataSourceRef: {apiGroup: kopiur.home-operations.com, kind: Restore, name: r-$1}
	YAML
}

main() {
	local keys=() claims=() k uid size policy_mover="" mover=""

	# Start from an empty namespace. A seed cannot initdb into an old data dir.
	if kubectl get ns "$ns" >/dev/null 2>&1; then
		kubectl delete ns "$ns" --wait=true --timeout=300s >/dev/null
	fi
	kubectl create ns "$ns" >/dev/null
	trap cleanup EXIT

	mapfile -t keys < <(python3 "$here/render.py" "$bp" --list)
	uid=$(python3 "$here/render.py" "$bp" --uid)
	for k in "${keys[@]}"; do
		claims+=("$k=r-$k")
	done

	if [[ -n $uid ]]; then
		policy_mover="  mover: {securityContext: {runAsUser: $uid, runAsGroup: $uid, runAsNonRoot: true}}"
		mover="  mover: {securityContext: {runAsUser: $uid, runAsGroup: $uid, runAsNonRoot: true}}"
	fi

	# 1. Repository and one SnapshotPolicy per volume.
	kopiur_repo | apply_yaml
	for k in "${keys[@]}"; do
		kopiur_policy "$k" "$policy_mover" | apply_yaml
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
		kopiur_snapshot "$k" | apply_yaml
		wait_jsonpath "$ns" snapshot "$k-1" '{.status.phase}' Succeeded \
			|| return 1
		kopiur_restore "$k" "$size" "$mover" | apply_yaml
		wait_jsonpath "$ns" restore "r-$k" '{.status.phase}' Completed \
			|| return 1
		wait_jsonpath "$ns" pvc "r-$k" '{.status.phase}' Bound || return 1
	done

	# 4. Run the check against the restored PVCs.
	log "running check $bp"
	"$here/check.sh" "$bp" "$ns" "check-${app//[^a-z0-9-]/-}" "${claims[@]}"
	local rc=$?
	log "result exit=$rc (0 = PASS)"
	return "$rc"
}

main
