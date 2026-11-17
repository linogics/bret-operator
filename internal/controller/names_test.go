package controller

import (
	"regexp"
	"strings"
	"testing"
)

var dnsLabelRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func TestBoundedNameLimits(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 60)
	got := boundedName(long, "-pgdata")

	if len(got) > 63 || !dnsLabelRE.MatchString(got) || !strings.HasSuffix(got, "-pgdata") {
		t.Errorf("boundedName = %q, want a DNS label of at most 63 chars ending in -pgdata", got)
	}

	if other := boundedName(strings.Repeat("a", 59)+"b", "-pgdata"); other == got {
		t.Errorf("different bases gave the same name %q", got)
	}

	if got := boundedName("r1", "-data"); got != "r1-data" {
		t.Errorf("short name changed: %q", got)
	}

	huge := boundedName(long, "-"+strings.Repeat("b", 50))
	if len(huge) > 63 || !dnsLabelRE.MatchString(huge) {
		t.Errorf("long suffix: %q, want a DNS label of at most 63 chars", huge)
	}

	if v := restoredVolumeName(strings.Repeat("c", 70)); len(v) > 63 || !dnsLabelRE.MatchString(v) {
		t.Errorf("restoredVolumeName = %q", v)
	}
}
