// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestEmbeddedDashboardsAreValid(t *testing.T) {
	titles := map[string]string{
		configMapName:      "OpenTelemetry Collector",
		genAIConfigMapName: "OpenTelemetry GenAI",
	}
	uids := map[string]bool{}

	for _, db := range dashboards() {
		t.Run(db.configMapName, func(t *testing.T) {
			var parsed struct {
				Title string `json:"title"`
				UID   string `json:"uid"`
				Rows  []struct {
					Panels []struct {
						Type string `json:"type"`
					} `json:"panels"`
				} `json:"rows"`
				Templating struct {
					List []struct {
						Name string `json:"name"`
					} `json:"list"`
				} `json:"templating"`
			}
			require.NoError(t, json.Unmarshal([]byte(db.content), &parsed))
			assert.Equal(t, titles[db.configMapName], parsed.Title)
			assert.NotEmpty(t, parsed.UID)
			assert.False(t, uids[parsed.UID], "dashboard uids must be unique")
			uids[parsed.UID] = true
			assert.NotEmpty(t, parsed.Rows)
			assert.NotEmpty(t, parsed.Templating.List)

			// The console renders only these panel types.
			supported := map[string]bool{"graph": true, "singlestat": true, "gauge": true, "table": true, "grafana-piechart-panel": true}
			for _, row := range parsed.Rows {
				for _, panel := range row.Panels {
					assert.True(t, supported[panel.Type], "unsupported panel type %q", panel.Type)
				}
			}
		})
	}
	assert.Len(t, uids, len(titles))
}

func TestDashboardManagement(t *testing.T) {
	// Start with a stale copy of one dashboard to cover the update path.
	stale := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: genAIConfigMapName, Namespace: openshiftDashboardsNamespace},
		Data:       map[string]string{"genai.json": "{}"},
	}
	clientset := fake.NewClientset(stale)
	configMaps := clientset.CoreV1().ConfigMaps(openshiftDashboardsNamespace)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewDashboardManagement(clientset).Start(ctx) }()

	require.Eventually(t, func() bool {
		for _, db := range dashboards() {
			cm, err := configMaps.Get(context.Background(), db.configMapName, metav1.GetOptions{})
			if err != nil || cm.Data[db.key] != db.content {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)

	for _, db := range dashboards() {
		cm, err := configMaps.Get(context.Background(), db.configMapName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Equal(t, "true", cm.Labels["console.openshift.io/dashboard"])
		assert.Len(t, cm.Data, 1)
	}

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the dashboard management did not stop")
	}

	list, err := configMaps.List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, list.Items, "the dashboards must be removed on shutdown")
}

func TestDashboardManagementNeedsLeaderElection(t *testing.T) {
	assert.True(t, dashboardManagement{}.NeedLeaderElection())
}
