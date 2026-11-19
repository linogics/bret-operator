#!/usr/bin/env bash
# Operator round trip with the WordPress example in examples/PROVIDER:
# deploy, back up both PVCs, run a Restore, then a RestoreTest.
# Run it from the repo root.
#
# usage: hack/check-test/operator-roundtrip.sh PROVIDER [negative]
#   PROVIDER   kopiur, volsync or velero.
#   negative   apply negative/restore.yaml (one source only), expect Failed.
# Needs the operator, the backup tool and the S3 endpoint from
# https://bret-operator.linogics.io/contributing/development/
# env: NS namespace (default bret-PROVIDER). KEEP=1 keeps it.
#      TIMEOUT per wait (default 900s). VELERO_NS (default velero).
#      S3_ACCESS_KEY, S3_SECRET_KEY, REPO_PASSWORD: test credentials.

provider=${1:-}
mode=${2:-}
dir=examples/$provider

if [[ ! -f $dir/restore.yaml ]]; then
	sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 2
fi

ns=${NS:-bret-$provider}
timeout=${TIMEOUT:-900s}
velero_ns=${VELERO_NS:-velero}
s3=versitygw.storage.svc.cluster.local:7070
access_key=${S3_ACCESS_KEY:-AKIDSPIKE}
secret_key=${S3_SECRET_KEY:-SECRETSPIKE12345}
password=${REPO_PASSWORD:-example-repo-password}

log() {
	echo "[$provider${mode:+/$mode}] $*"
}

k() {
	kubectl -n "$ns" "$@"
}

# Waits until the jsonpath of an object has the value.
wait_for() {
	local path=$1 value=$2

	shift 2
	k wait --for=jsonpath="$path"="$value" --timeout="$timeout" "$@"
}

cleanup() {
	if [[ -z ${KEEP:-} ]]; then
		kubectl delete namespace "$ns" --ignore-not-found --wait=false
	fi
}

# The S3 credentials of the backup tool. Velero keeps its own.
create_secrets() {
	local pvc

	case $provider in
	kopiur)
		k create secret generic kopiur-repo-creds \
			--from-literal=AWS_ACCESS_KEY_ID="$access_key" \
			--from-literal=AWS_SECRET_ACCESS_KEY="$secret_key" \
			--from-literal=KOPIA_PASSWORD="$password"
		;;
	volsync)
		for pvc in html mysql; do
			k create secret generic "restic-$pvc" \
				--from-literal=RESTIC_REPOSITORY="s3:http://$s3/volsync/$ns/$pvc" \
				--from-literal=RESTIC_PASSWORD="$password" \
				--from-literal=AWS_ACCESS_KEY_ID="$access_key" \
				--from-literal=AWS_SECRET_ACCESS_KEY="$secret_key" \
				|| return 1
		done
		;;
	esac
}

take_backup() {
	case $provider in
	kopiur)
		k apply -f "$dir/snapshot.yaml" || return 1
		wait_for '{.status.phase}' Succeeded snapshot --all
		;;
	volsync)
		# A manual trigger replaces the nightly schedule for one sync.
		k patch replicationsource html mysql --type=merge \
			-p '{"spec":{"trigger":{"schedule":null,"manual":"wordpress-1"}}}' \
			|| return 1
		wait_for '{.status.lastManualSync}' wordpress-1 replicationsource --all
		;;
	velero)
		# Backup names are global in Velero. Drop the one of an earlier run.
		if kubectl -n "$velero_ns" get backup wordpress-1 >/dev/null 2>&1; then
			velero backup delete wordpress-1 -n "$velero_ns" --confirm \
				|| return 1
			kubectl -n "$velero_ns" wait --for=delete backup/wordpress-1 \
				--timeout="$timeout" || return 1
		fi

		velero backup create wordpress-1 -n "$velero_ns" \
			--include-namespaces "$ns" --snapshot-move-data \
			--labels velero.io/schedule-name=wordpress --wait || return 1

		# --wait exits 0 on PartiallyFailed too.
		kubectl -n "$velero_ns" wait --for=jsonpath='{.status.phase}'=Completed \
			--timeout="$timeout" backup/wordpress-1
		;;
	esac
}

negative() {
	log '4. Restore without mysql, expect Failed'
	k apply -f "$dir/negative/restore.yaml" || return 1
	wait_for '{.status.phase}' Failed restore/wordpress-negative || return 1
	k get restore wordpress-negative \
		-o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}{"\n"}'
}

positive() {
	log '4. Restore, expect Succeeded'
	k apply -f "$dir/restore.yaml" || return 1
	wait_for '{.status.phase}' Succeeded restore/wordpress-manual || return 1

	log '5. RestoreTest run-now, expect Succeeded'
	k apply -f "$dir/restoretest.yaml" || return 1
	k annotate --overwrite restoretest/wordpress \
		bret.linogics.io/run-now="$(date +%s)" || return 1
	wait_for '{.status.lastRun.result}' Succeeded restoretest/wordpress
}

main() {
	trap cleanup EXIT

	log "1. deploy WordPress into $ns"
	kubectl delete namespace "$ns" --ignore-not-found --timeout="$timeout"
	kubectl create namespace "$ns" || return 1
	create_secrets || return 1
	k apply -k "$dir" || return 1

	log '2. wait for the app and the seed'
	k wait --for=condition=Available deployment --all --timeout="$timeout" \
		|| return 1
	k wait --for=condition=Complete job/seed --timeout="$timeout" || return 1

	log '3. back up html and mysql'
	take_backup || return 1

	if [[ $mode == negative ]]; then
		negative || return 1
	else
		positive || return 1
	fi

	log PASS
}

main
