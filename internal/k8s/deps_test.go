package k8s

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestPodRefs(t *testing.T) {
	optional := true
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			ServiceAccountName: "api-sa",
			ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "regcred"}},
			InitContainers: []corev1.Container{{
				EnvFrom: []corev1.EnvFromSource{{
					ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "init-cm"}},
				}},
			}},
			Containers: []corev1.Container{{
				EnvFrom: []corev1.EnvFromSource{
					{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-cm"}}},
					{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-secret"}, Optional: &optional}},
				},
				Env: []corev1.EnvVar{{
					Name: "TOKEN",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "token-secret"}},
					},
				}},
			}},
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data-pvc"}}},
				{Name: "cfg", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "vol-cm"}}}},
				{Name: "proj", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
					{Secret: &corev1.SecretProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "proj-secret"}}},
				}}}},
			},
		},
	}

	cms, secs, pvcs, sa := podRefs(pod)

	wantCM := map[string]bool{"init-cm": true, "app-cm": true, "vol-cm": true}
	if len(cms) != len(wantCM) {
		t.Fatalf("configmaps: got %v, want keys %v", cms, wantCM)
	}
	for _, r := range cms {
		if !wantCM[r.name] {
			t.Errorf("unexpected configmap %q", r.name)
		}
	}

	wantSec := map[string]bool{"regcred": true, "app-secret": true, "token-secret": true, "proj-secret": true}
	if len(secs) != len(wantSec) {
		t.Fatalf("secrets: got %v, want keys %v", secs, wantSec)
	}
	for _, r := range secs {
		if !wantSec[r.name] {
			t.Errorf("unexpected secret %q", r.name)
		}
		if r.name == "app-secret" && !r.optional {
			t.Errorf("app-secret should be optional")
		}
		if r.name == "token-secret" && r.optional {
			t.Errorf("token-secret should be required")
		}
	}

	if len(pvcs) != 1 || pvcs[0] != "data-pvc" {
		t.Errorf("pvcs: got %v, want [data-pvc]", pvcs)
	}
	if sa != "api-sa" {
		t.Errorf("sa: got %q, want api-sa", sa)
	}
}

func TestPodRefsDefaultServiceAccount(t *testing.T) {
	_, _, _, sa := podRefs(&corev1.Pod{})
	if sa != "default" {
		t.Errorf("empty serviceAccountName should resolve to default, got %q", sa)
	}
}
