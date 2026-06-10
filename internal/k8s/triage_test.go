package k8s

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func waiting(name, reason string, restarts int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:         name,
		RestartCount: restarts,
		State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: reason},
		},
	}
}

func running(name string) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name:  name,
		Ready: true,
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
	}
}

func terminated(name string, exit int32) corev1.ContainerStatus {
	return corev1.ContainerStatus{
		Name: name,
		State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{Reason: "Completed", ExitCode: exit},
		},
	}
}

func TestPodIssue(t *testing.T) {
	pod := func(phase corev1.PodPhase, init, main []corev1.ContainerStatus) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
			Status: corev1.PodStatus{
				Phase:                 phase,
				InitContainerStatuses: init,
				ContainerStatuses:     main,
			},
		}
	}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want string // substring expected in the issue; "" = healthy
	}{
		{
			name: "healthy running pod",
			pod:  pod(corev1.PodRunning, nil, []corev1.ContainerStatus{running("app")}),
			want: "",
		},
		{
			name: "healthy pod with completed init container",
			pod: pod(corev1.PodRunning,
				[]corev1.ContainerStatus{terminated("migrate", 0)},
				[]corev1.ContainerStatus{running("app")}),
			want: "",
		},
		{
			name: "transient ContainerCreating is ignored",
			pod:  pod(corev1.PodPending, nil, []corev1.ContainerStatus{waiting("app", "ContainerCreating", 0)}),
			want: "",
		},
		{
			name: "transient PodInitializing is ignored",
			pod: pod(corev1.PodPending,
				[]corev1.ContainerStatus{running("migrate")},
				[]corev1.ContainerStatus{waiting("app", "PodInitializing", 0)}),
			want: "",
		},
		{
			name: "main container CrashLoopBackOff",
			pod:  pod(corev1.PodRunning, nil, []corev1.ContainerStatus{waiting("app", "CrashLoopBackOff", 3)}),
			want: "app Waiting(CrashLoopBackOff)",
		},
		{
			name: "init container CrashLoopBackOff",
			pod: pod(corev1.PodPending,
				[]corev1.ContainerStatus{waiting("migrate", "CrashLoopBackOff", 4)},
				[]corev1.ContainerStatus{waiting("app", "PodInitializing", 0)}),
			want: "migrate Waiting(CrashLoopBackOff)",
		},
		{
			name: "init container ImagePullBackOff",
			pod: pod(corev1.PodPending,
				[]corev1.ContainerStatus{waiting("migrate", "ImagePullBackOff", 0)},
				nil),
			want: "migrate Waiting(ImagePullBackOff)",
		},
		{
			name: "terminated with nonzero exit",
			pod:  pod(corev1.PodRunning, nil, []corev1.ContainerStatus{terminated("app", 1)}),
			want: "app Terminated(Completed exit=1)",
		},
		{
			name: "high restart count",
			pod: pod(corev1.PodRunning, nil, []corev1.ContainerStatus{{
				Name:         "app",
				Ready:        true,
				RestartCount: 7,
				State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}}),
			want: "app restarts=7",
		},
		{
			name: "failed phase with no container detail",
			pod:  pod(corev1.PodFailed, nil, nil),
			want: "phase=Failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := podIssue(tt.pod)
			if tt.want == "" {
				if got != "" {
					t.Fatalf("podIssue() = %q, want healthy (empty)", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("podIssue() = %q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestWriteCategory(t *testing.T) {
	var b strings.Builder
	if n := writeCategory(&b, "Empty", nil); n != 0 {
		t.Fatalf("writeCategory(empty) = %d, want 0", n)
	}
	if b.Len() != 0 {
		t.Fatalf("writeCategory(empty) wrote %q, want nothing", b.String())
	}

	lines := make([]string, maxPerCategory+5)
	for i := range lines {
		lines[i] = "issue"
	}
	if n := writeCategory(&b, "Capped", lines); n != len(lines) {
		t.Fatalf("writeCategory(capped) = %d, want %d (full count, not capped)", n, len(lines))
	}
	out := b.String()
	if !strings.Contains(out, fmt.Sprintf("## Capped (%d)", len(lines))) {
		t.Fatalf("missing header with full count:\n%s", out)
	}
	if !strings.Contains(out, "…and 5 more") {
		t.Fatalf("missing overflow marker:\n%s", out)
	}
	if got := strings.Count(out, "- issue"); got != maxPerCategory {
		t.Fatalf("rendered %d entries, want %d", got, maxPerCategory)
	}
}
