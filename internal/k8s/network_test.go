package k8s

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func svc(t corev1.ServiceType, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		Spec: corev1.ServiceSpec{Type: t, Selector: selector},
	}
}

func TestServiceFindings(t *testing.T) {
	withSel := map[string]string{"app": "api"}

	tests := []struct {
		name        string
		svc         *corev1.Service
		matched     int
		ready       int
		readyEPs    int
		notReadyEPs int
		policies    []string
		want        string // substring expected; "" = no findings
	}{
		{
			name:    "healthy service",
			svc:     svc(corev1.ServiceTypeClusterIP, withSel),
			matched: 3, ready: 3, readyEPs: 3,
			want: "",
		},
		{
			name:    "selector matches no pods",
			svc:     svc(corev1.ServiceTypeClusterIP, withSel),
			matched: 0, ready: 0, readyEPs: 0,
			want: "matches 0 pods",
		},
		{
			name:    "pods match but none ready",
			svc:     svc(corev1.ServiceTypeClusterIP, withSel),
			matched: 2, ready: 0, readyEPs: 0,
			want: "none are Ready",
		},
		{
			name:    "partial capacity",
			svc:     svc(corev1.ServiceTypeClusterIP, withSel),
			matched: 3, ready: 2, readyEPs: 2, notReadyEPs: 1,
			want: "not ready alongside",
		},
		{
			name:     "no selector, no endpoints",
			svc:      svc(corev1.ServiceTypeClusterIP, nil),
			readyEPs: -1,
			want:     "manually-managed",
		},
		{
			name:    "loadbalancer without address",
			svc:     svc(corev1.ServiceTypeLoadBalancer, withSel),
			matched: 1, ready: 1, readyEPs: 1,
			want: "no external address",
		},
		{
			name:     "externalname is never flagged",
			svc:      svc(corev1.ServiceTypeExternalName, nil),
			readyEPs: -1,
			want:     "",
		},
		{
			name:    "networkpolicy is surfaced",
			svc:     svc(corev1.ServiceTypeClusterIP, withSel),
			matched: 1, ready: 1, readyEPs: 1,
			policies: []string{"default-deny"},
			want:     "NetworkPolicy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceFindings(tc.svc, tc.matched, tc.ready, tc.readyEPs, tc.notReadyEPs, tc.policies)
			joined := strings.Join(got, "\n")
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected no findings, got: %v", got)
				}
				return
			}
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("findings %q do not contain %q", joined, tc.want)
			}
		})
	}
}
