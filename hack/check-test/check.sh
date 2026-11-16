#!/usr/bin/env bash
# Run one RestoreCheck check as a Pod against already restored PVCs.
# Verdict = pod phase: Succeeded (every check container exit 0) passes.
#
# Run it from the repo root.
#
# usage: hack/check-test/check.sh CHECK NAMESPACE POD_NAME KEY=PVC [KEY=PVC ...]
#   KEY is the restoredVolumes sourcePVC.
# env: TIMEOUT (seconds, default 900), KEEP=1 keeps the pod after the run.

check_usage() {
	if [[ $# -lt 4 ]]; then
		echo "usage: check.sh CHECK NAMESPACE POD_NAME KEY=PVC..." >&2
		exit 2
	fi
}

wait_pod() {
	local ns=$1 pod=$2 end phase=""

	end=$((SECONDS + ${TIMEOUT:-900}))

	while ((SECONDS < end)); do
		phase=$(kubectl -n "$ns" get pod "$pod" \
			-o jsonpath='{.status.phase}' 2>/dev/null)

		if [[ $phase == Succeeded || $phase == Failed ]]; then
			break
		fi

		sleep 5
	done

	printf '%s\n' "${phase:-timeout}"
}

print_logs() {
	local ns=$1 pod=$2 c containers

	kubectl -n "$ns" get pod "$pod" \
		-o jsonpath='{range .status.initContainerStatuses[*]}init {.name} exit={.state.terminated.exitCode} reason={.state.terminated.reason}{"\n"}{end}{range .status.containerStatuses[*]}main {.name} exit={.state.terminated.exitCode} reason={.state.terminated.reason}{"\n"}{end}' \
		2>/dev/null

	echo "== logs (tail)"
	containers=$(kubectl -n "$ns" get pod "$pod" \
		-o jsonpath='{.spec.initContainers[*].name} {.spec.containers[*].name}')

	for c in $containers; do
		echo "--- $c"
		kubectl -n "$ns" logs "$pod" -c "$c" --tail=40 2>&1
	done
}

main() {
	check_usage "$@"

	local bp=$1 ns=$2 pod=$3 phase

	shift 3

	if ! kubectl get ns "$ns" >/dev/null 2>&1; then
		kubectl create ns "$ns" >/dev/null
	fi

	kubectl -n "$ns" delete pod "$pod" --ignore-not-found --wait=true >/dev/null

	if ! python3 hack/check-test/render.py "$bp" --name "$pod" --ns "$ns" \
		--claims "$@" | kubectl apply -f - >/dev/null; then
		echo "render or apply failed" >&2

		exit 2
	fi

	phase=$(wait_pod "$ns" "$pod")

	echo "== containers ($pod)"
	print_logs "$ns" "$pod"

	if [[ -z ${KEEP:-} ]]; then
		kubectl -n "$ns" delete pod "$pod" --ignore-not-found >/dev/null
	fi

	if [[ $phase == Succeeded ]]; then
		echo "VERDICT: PASS ($pod)"

		return 0
	fi

	echo "VERDICT: FAIL ($pod, phase=$phase)"
	return 1
}

main "$@"
