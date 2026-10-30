// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package openshift

import (
	"context"
	_ "embed"
	"errors"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// The dashboards are created manually following the syntax from Grafana 5. For development purposes, a dashboard can be created just by loading the JSON file
// in a ConfigMap from the openshift-config-managed and adding the console.openshift.io/dashboard=true label.
//
//go:embed metrics-dashboard.json
var dashboardContent string

//go:embed genai-dashboard.json
var genAIDashboardContent string

const (
	openshiftDashboardsNamespace = "openshift-config-managed"
	configMapName                = "opentelemetry-collector"
	genAIConfigMapName           = "opentelemetry-genai"
)

// dashboard describes a console dashboard published as a ConfigMap.
type dashboard struct {
	configMapName string
	key           string
	content       string
}

// dashboards returns the dashboards managed by the operator: the OpenTelemetry Collector health dashboard
// and the OpenTelemetry GenAI dashboard.
func dashboards() []dashboard {
	return []dashboard{
		{configMapName: configMapName, key: "otel.json", content: dashboardContent},
		{configMapName: genAIConfigMapName, key: "genai.json", content: genAIDashboardContent},
	}
}

type dashboardManagement struct {
	clientset kubernetes.Interface
}

var _ manager.Runnable = (*dashboardManagement)(nil)

func NewDashboardManagement(clientset kubernetes.Interface) manager.Runnable {
	return dashboardManagement{
		clientset: clientset,
	}
}

func (d dashboardManagement) Start(ctx context.Context) error {
	log := ctrl.Log.WithName("openshift-dashboards")

	for _, db := range dashboards() {
		if err := d.apply(ctx, db); err != nil {
			// A dashboard that cannot be created must not stop the operator or the other dashboards.
			log.Error(err, "failed to create the OpenShift dashboard", "configmap", db.configMapName)
		}
	}

	<-ctx.Done()

	var errs []error
	for _, db := range dashboards() {
		// The context is cancelled at this point, so the clean up must not use it.
		err := d.clientset.CoreV1().ConfigMaps(openshiftDashboardsNamespace).Delete(context.Background(), db.configMapName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// apply creates the dashboard ConfigMap, or updates it when it already exists.
func (d dashboardManagement) apply(ctx context.Context, db dashboard) error {
	cm := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      db.configMapName,
			Namespace: openshiftDashboardsNamespace,
			Labels: map[string]string{
				"console.openshift.io/dashboard": "true",
			},
		},
		Data: map[string]string{
			db.key: db.content,
		},
	}

	configMaps := d.clientset.CoreV1().ConfigMaps(openshiftDashboardsNamespace)
	_, err := configMaps.Get(ctx, db.configMapName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = configMaps.Create(ctx, &cm, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	// config map already exists, update it
	_, err = configMaps.Update(ctx, &cm, metav1.UpdateOptions{})
	return err
}

func (dashboardManagement) NeedLeaderElection() bool {
	return true
}
