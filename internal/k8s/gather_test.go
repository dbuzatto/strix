package k8s

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantName string
		wantErr  bool
	}{
		{"pod/api-7d9f", "pod", "api-7d9f", false},
		{"po/api", "pod", "api", false},
		{"deploy/api", "deployment", "api", false},
		{"sts/postgres", "statefulset", "postgres", false},
		{"statefulsets/pg", "statefulset", "pg", false},
		{"ds/fluentd", "daemonset", "fluentd", false},
		{"job/migrate", "job", "migrate", false},
		{"cj/backup", "cronjob", "backup", false},
		{"cronjob/backup", "cronjob", "backup", false},
		{"node/worker-3", "node", "worker-3", false},
		{"no/worker-3", "node", "worker-3", false},
		{"NODE/worker-3", "node", "worker-3", false}, // case-insensitive kind
		{"ingress/api", "", "", true},                // unsupported kind
		{"api", "", "", true},                        // missing kind/name separator
		{"pod/", "", "", true},                       // empty name
		{"/api", "", "", true},                       // empty kind
	}
	for _, tc := range cases {
		ref, err := ParseRef(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseRef(%q): expected error, got %+v", tc.in, ref)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRef(%q): unexpected error: %v", tc.in, err)
			continue
		}
		if ref.Kind != tc.wantKind || ref.Name != tc.wantName {
			t.Errorf("ParseRef(%q) = {%s %s}, want {%s %s}", tc.in, ref.Kind, ref.Name, tc.wantKind, tc.wantName)
		}
	}
}

func TestTruncateTail(t *testing.T) {
	// Short input passes through untouched.
	if got, trunc := truncateTail("a\nb\nc", 100); got != "a\nb\nc" || trunc {
		t.Fatalf("short input: got %q trunc=%v", got, trunc)
	}
	// Long input keeps the tail and starts at a line boundary.
	text := "DROPPED line one\nkeep two\nkeep three"
	got, trunc := truncateTail(text, len("e\nkeep two\nkeep three"))
	if !trunc {
		t.Fatal("expected truncation")
	}
	if got != "keep two\nkeep three" {
		t.Fatalf("got %q, want tail starting at line boundary", got)
	}
	// A single huge line (no newline in the kept window) is kept as a slice.
	got, trunc = truncateTail("xxxxxxxxxxyyyyy", 5)
	if !trunc || got != "yyyyy" {
		t.Fatalf("single line: got %q trunc=%v", got, trunc)
	}
}
