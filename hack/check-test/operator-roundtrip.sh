#!/usr/bin/env bash
# Real round trip through the operator: seed, back up, apply check + Restore,
# wait for the Restore verdict, compare it with the expectation.
#
# usage: operator-roundtrip.sh PROVIDER APP [NEGATIVE]
#   PROVIDER kopiur|volsync|velero, APP has seeds/APP.yaml and checks/APP.yaml.
#   NEGATIVE=1 (3rd arg or env) breaks the Restore on purpose and expects Failed:
#     kopiur/volsync: omit the last source          -> SourceMissing
#     velero: label only the first PVC              -> RestoredVolumeMissing
#   Needs an app with at least two PVCs.
# Needs the operator running with the provider enabled, the backup tool
# installed and the S3 endpoint from docs/development.md.
# env: KEEP=1 keeps namespace bpo-PROVIDER-APP. TIMEOUT per wait (default 900).
#      VELERO_NS Velero namespace (default velero).
#      STORAGE_CLASS (default csi-hostpath-sc), SNAP_CLASS (csi-hostpath-snapclass).

here=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$here/../.." && pwd)

usage() {
	sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
	exit "${1:-2}"
}

if [[ ${1:-} == -h || ${1:-} == --help ]]; then
	usage 0
fi
if [[ $# -lt 2 || $# -gt 3 ]]; then
	usage
fi
provider=$1
app=$2
negative=${3:-${NEGATIVE:-}}
[[ $negative == 0 ]] && negative=
case $provider in
kopiur | volsync | velero) ;;
*) usage ;;
esac

seed=$here/seeds/$app.yaml
bp=$repo_root/checks/$app.yaml
for f in "$seed" "$bp"; do
	if [[ ! -f $f ]]; then
		echo "missing $f" >&2
		exit 2
	fi
done

ns="bpo-$provider-$app"
timeout_s=${TIMEOUT:-900}
velero_ns=${VELERO_NS:-velero}
sc=${STORAGE_CLASS:-csi-hostpath-sc}
snapclass=${SNAP_CLASS:-csi-hostpath-snapclass}
s3=versitygw.storage.svc.cluster.local:7070
restore=rt
velero_backup=
label=bret.linogics.io/test

log() {
	echo "[$provider/$app] $*"
}

wait_jsonpath() {
	# args: namespace kind name jsonpath want (want is a glob, "?*" = any value)
	local ns=$1 kind=$2 name=$3 path=$4 want=$5 end got=""

	end=$((SECONDS + timeout_s))
	while ((SECONDS < end)); do
		got=$(kubectl -n "$ns" get "$kind" "$name" -o jsonpath="$path" \
			2>/dev/null)
		# shellcheck disable=SC2053
		if [[ $got == $want ]]; then
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

mover_block() {
	# args: uid, kind (policy|restore). Prints nothing when uid is empty.
	local uid=$1 kind=$2

	if [[ -z $uid ]]; then
		return 0
	fi
	if [[ $kind == restore ]]; then
		printf '  mover: {securityContext: {runAsUser: %s, runAsGroup: %s, runAsNonRoot: true}, podSecurityContext: {fsGroup: %s}}\n' \
			"$uid" "$uid" "$uid"
		return 0
	fi
	printf '  mover: {securityContext: {runAsUser: %s, runAsGroup: %s, runAsNonRoot: true}}\n' \
		"$uid" "$uid"
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
		      prefix: bpo/$provider-$app/
		      endpoint: $s3
		      region: us-east-1
		      tls: {disableTls: true}
		      auth: {secretRef: {name: kopiur-repo-creds}}
		  encryption:
		    passwordSecretRef: {name: kopiur-repo-creds, key: KOPIA_PASSWORD}
		  create: {enabled: true}
	YAML
}

kopiur_backup() {
	# args: pvc, mover block (may be empty)
	cat <<-YAML
		apiVersion: kopiur.home-operations.com/v1alpha1
		kind: SnapshotPolicy
		metadata: {name: $1}
		spec:
		  repository: {kind: Repository, name: primary}
		  sources:
		    - pvc: {name: $1}
		  copyMethod: Snapshot
		  volumeSnapshotClassName: $snapclass
		  retention: {keepDaily: 1}
		$2
		---
		apiVersion: kopiur.home-operations.com/v1alpha1
		kind: Snapshot
		metadata:
		  name: $1-1
		  labels: {app.kubernetes.io/name: $app-$1}
		spec: {policyRef: {name: $1}, deletionPolicy: Retain}
	YAML
}

volsync_repo() {
	# args: pvc. One repository per PVC: a shared one races on init and mixes snapshots.
	cat <<-YAML
		apiVersion: v1
		kind: Secret
		metadata: {name: restic-config-$1}
		type: Opaque
		stringData:
		  RESTIC_REPOSITORY: "s3:http://$s3/volsync/bpo/$provider-$app/$1"
		  RESTIC_PASSWORD: "spike-restic-pass"
		  AWS_ACCESS_KEY_ID: "AKIDSPIKE"
		  AWS_SECRET_ACCESS_KEY: "SECRETSPIKE12345"
	YAML
}

volsync_backup() {
	# args: pvc, uid (may be empty)
	cat <<-YAML
		apiVersion: volsync.backube/v1alpha1
		kind: ReplicationSource
		metadata: {name: $1-backup}
		spec:
		  sourcePVC: $1
		  trigger: {manual: once}
		  restic:
		    repository: restic-config-$1
		    copyMethod: Snapshot
		    volumeSnapshotClassName: $snapclass
		    retain: {daily: 1}
		$([[ -n $2 ]] && printf '    moverSecurityContext: {runAsUser: %s, runAsGroup: %s, fsGroup: %s}' "$2" "$2" "$2")
	YAML
}

restore_source() {
	# args: pvc. One source for kopiur/volsync.
	local ref=$1-1
	[[ $provider == volsync ]] && ref=$1-backup
	cat <<-YAML
		    - sourcePVC: $1
		      backupRef: {name: $ref}
		      storageClassName: $sc
	YAML
}

restore_yaml() {
	# args: check name, then the pvc keys to restore (kopiur/volsync)
	# or the backup name (velero).
	local check=$1 k
	shift
	cat <<-YAML
		apiVersion: bret.linogics.io/v1alpha1
		kind: Restore
		metadata: {name: $restore}
		spec:
		  provider: $provider
		  restoreCheckRef: {name: $check}
		  sources:
	YAML
	if [[ $provider == velero ]]; then
		cat <<-YAML
			    - backupRef: {name: $1}
			      labelSelector: {matchLabels: {$label: $app}}
		YAML
		return 0
	fi
	for k in "$@"; do
		restore_source "$k"
	done
}

dump_logs() {
	# Check pods live in ns, or in bret-scratch-* for velero.
	local n p

	for n in "$ns" $(kubectl get ns -o name | sed -n 's#^namespace/\(bret-scratch-.*\)#\1#p'); do
		for p in $(kubectl -n "$n" get pods -o name 2>/dev/null); do
			echo "--- $n/$p"
			kubectl -n "$n" logs "$p" --all-containers --tail=40 2>&1
		done
	done
}

backup() {
	# args: pvc keys. Back up each PVC with the provider's own tool.
	local k uid first=1 name

	case $provider in
	kopiur)
		kopiur_repo | apply_yaml
		for k in "$@"; do
			uid=$(python3 "$here/render.py" "$bp" --uid "$k")
			kopiur_backup "$k" "$(mover_block "$uid" policy)" | apply_yaml
		done
		for k in "$@"; do
			wait_jsonpath "$ns" snapshot "$k-1" '{.status.phase}' Succeeded \
				|| return 1
		done
		;;
	volsync)
		for k in "$@"; do
			volsync_repo "$k" | apply_yaml
			uid=$(python3 "$here/render.py" "$bp" --uid "$k")
			volsync_backup "$k" "$uid" | apply_yaml
		done
		for k in "$@"; do
			wait_jsonpath "$ns" replicationsource "$k-backup" \
				'{.status.lastSyncTime}' '?*' || return 1
		done
		;;
	velero)
		for k in "$@"; do
			if [[ -n $negative && $first == 0 ]]; then
				break
			fi
			kubectl -n "$ns" label pvc "$k" "$label=$app" --overwrite >/dev/null
			first=0
		done
		name="bpo-$app-$(date +%s)"
		velero_backup=$name
		velero backup create "$name" --namespace "$velero_ns" \
			--include-namespaces "$ns" --snapshot-move-data >/dev/null || return 1
		wait_jsonpath "$velero_ns" backup "$name" '{.status.phase}' Completed \
			|| return 1
		;;
	esac
}

main() {
	local keys=() restore_keys=() expect_phase=Succeeded expect_reason="" check
	local phase reason end

	if [[ -n $negative ]]; then
		expect_phase=Failed
		expect_reason=SourceMissing
		[[ $provider == velero ]] && expect_reason=RestoredVolumeMissing
	fi

	# Start from an empty namespace. A seed cannot initdb into an old data dir.
	if kubectl get ns "$ns" >/dev/null 2>&1; then
		kubectl delete ns "$ns" --wait=true --timeout=300s >/dev/null
	fi
	kubectl create ns "$ns" >/dev/null
	trap cleanup EXIT

	mapfile -t keys < <(python3 "$here/render.py" "$bp" --list)
	if [[ -n $negative && ${#keys[@]} -lt 2 ]]; then
		log "NEGATIVE needs an app with two or more PVCs"
		return 2
	fi

	# 1. Seed the source data.
	log "seeding from $seed"
	kubectl -n "$ns" apply -f "$seed" >/dev/null
	if ! wait_jsonpath "$ns" pod seed '{.status.phase}' Succeeded; then
		kubectl -n "$ns" logs seed --all-containers --tail=80
		return 1
	fi
	kubectl -n "$ns" delete pod seed --ignore-not-found >/dev/null

	# 2. Back up every PVC.
	log "backing up ${keys[*]}"
	backup "${keys[@]}" || return 1

	# 3. Apply the check and the Restore. The check file names namespace
	# "default"; drop it so kubectl uses ours.
	check=$(sed -n 's/^  name: //p' "$bp" | head -1)
	sed '/^  namespace: /d' "$bp" | apply_yaml
	restore_keys=("${keys[@]}")
	if [[ -n $negative && $provider != velero ]]; then
		unset 'restore_keys[-1]'
	fi
	if [[ $provider == velero ]]; then
		restore_yaml "$check" "$velero_backup" | apply_yaml
	else
		restore_yaml "$check" "${restore_keys[@]}" | apply_yaml
	fi

	# 4. Wait for a final phase.
	end=$((SECONDS + timeout_s))
	phase=""
	while ((SECONDS < end)); do
		phase=$(kubectl -n "$ns" get restore "$restore" \
			-o jsonpath='{.status.phase}' 2>/dev/null)
		[[ $phase == Succeeded || $phase == Failed || $phase == Canceled ]] && break
		sleep 5
	done
	reason=$(kubectl -n "$ns" get restore "$restore" \
		-o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}')
	log "phase=${phase:-timeout} Ready.reason=$reason"
	dump_logs

	# 5. Compare with the expectation.
	if [[ $phase == "$expect_phase" && ( -z $expect_reason || $reason == "$expect_reason" ) ]]; then
		log "PASS (expected $expect_phase ${expect_reason})"
		return 0
	fi
	log "FAIL (expected $expect_phase ${expect_reason})"
	return 1
}

main
