package helper

import (
	"context"
	"maps"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestUpdatePGServiceKeepsLabelsOfChartDeployedService(t *testing.T) {
	const namespace = "pgskipper"
	t.Setenv("WATCH_NAMESPACE", namespace)

	chartLabels := map[string]string{
		"app.kubernetes.io/instance":   "pgskipper-services",
		"app.kubernetes.io/managed-by": "Helm",
	}
	service := func(name string) *corev1.Service {
		return &corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    maps.Clone(chartLabels),
		}}
	}
	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(service("dbaas-postgres-adapter"), service("postgres-operator")).
		Build()
	rm := &ResourceManager{kubeClient: kubeClient, name: "patroni-services", kind: "PatroniServices"}

	if err := rm.UpdatePGService(); err != nil {
		t.Fatalf("UpdatePGService: %v", err)
	}

	get := func(name string) *corev1.Service {
		t.Helper()
		got := &corev1.Service{}
		key := types.NamespacedName{Name: name, Namespace: namespace}
		if err := kubeClient.Get(context.Background(), key, got); err != nil {
			t.Fatalf("get service %s: %v", name, err)
		}
		return got
	}

	adapter := get("dbaas-postgres-adapter")
	if !maps.Equal(adapter.Labels, chartLabels) {
		t.Errorf("chart-deployed service labels = %v, want %v", adapter.Labels, chartLabels)
	}
	if len(adapter.OwnerReferences) != 1 {
		t.Errorf("chart-deployed service owner references = %v, want one", adapter.OwnerReferences)
	}

	operator := get("postgres-operator")
	if got := operator.Labels["app.kubernetes.io/managed-by"]; got != "operator" {
		t.Errorf("operator-managed service managed-by label = %q, want %q", got, "operator")
	}
}
