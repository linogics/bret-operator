#!/usr/bin/env bash
# Fails when the docs site misses a reason or metric the code can emit.
# Run it from the repository root (make docs-facts does).
#
# Reasons: every `reason... = "Name"` constant in api/ and internal/.
# Events reuse these reasons.
# Metrics: every "bret_..." name in internal/.
# A reason must appear as `Name` on the failure reasons page or the
# metrics and events page. A metric must appear as `name` on the
# metrics and events page.

pages='docs/src/content/docs/reference'
failures="$pages/failure-reasons.md"
metrics_page="$pages/metrics-and-events.md"

# Prints the non-test Go files under api/ and internal/, one per line.
go_sources() {
	find api internal -name '*.go' ! -name '*_test.go'
}

# Prints every reason constant value, one per line, sorted.
list_reasons() {
	go_sources \
		| xargs grep -hoE '^\s*[rR]eason[A-Za-z]*\s*=\s*"[A-Za-z]+"' \
		| grep -oE '"[A-Za-z]+"' \
		| tr -d '"' \
		| sort -u
}

# Prints every metric name, one per line, sorted.
list_metrics() {
	go_sources \
		| xargs grep -hoE '"bret_[a-z_]+"' \
		| tr -d '"' \
		| sort -u
}

# True when `name` appears in any of the given files.
mentioned() {
	local name=$1
	shift
	grep -qF "\`$name\`" "$@" 2>/dev/null
}

if [[ ! -d api || ! -d internal ]]; then
	echo 'run from the repository root'
	exit 2
fi

mapfile -t reasons < <(list_reasons)
mapfile -t metrics < <(list_metrics)

if ((${#reasons[@]} == 0 || ${#metrics[@]} == 0)); then
	echo 'found no reasons or no metrics in the code, the patterns are stale'
	exit 2
fi

missing=0

for reason in "${reasons[@]}"; do
	if ! mentioned "$reason" "$failures" "$metrics_page"; then
		echo "missing reason: $reason"
		missing=1
	fi
done

for metric in "${metrics[@]}"; do
	if ! mentioned "$metric" "$metrics_page"; then
		echo "missing metric: $metric"
		missing=1
	fi
done

if ((missing)); then
	echo "add them to $failures or $metrics_page"
	exit 1
fi

echo "docs cover ${#reasons[@]} reasons and ${#metrics[@]} metrics"
