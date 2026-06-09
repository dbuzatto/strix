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
