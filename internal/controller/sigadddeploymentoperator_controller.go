/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metrics "k8s.io/metrics/pkg/client/clientset/versioned"

	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	_ "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	_ "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	_ "sigs.k8s.io/controller-runtime/pkg/source"
	"time"

	sigDepOpv1alpha1 "github.com/Zzlllh/deployment-operator-with-pod-metrics/api/v1alpha1"
)

// SigAddDeploymentOperatorReconciler reconciles a SigAddDeploymentOperator object
type SigAddDeploymentOperatorReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	MetricsClient *metrics.Clientset

	// Prometheus metrics
	podCPUUsage    *prometheus.GaugeVec
	podMemoryUsage *prometheus.GaugeVec
}

// +kubebuilder:rbac:groups=cache.sig.com,resources=sigadddeploymentoperators,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cache.sig.com,resources=sigadddeploymentoperators/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cache.sig.com,resources=sigadddeploymentoperators/finalizers,verbs=update

// Grants permissions to manage Deployments in all namespaces
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch

// Grants permissions to manage Pods in all namespaces
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch

// Grants permissions to access Pod Metrics in all namespaces
// This assumes you're using the metrics.k8s.io API
// +kubebuilder:rbac:groups=metrics.k8s.io,resources=pods,verbs=get;list;watch

func (r *SigAddDeploymentOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	SigDepOperator := &sigDepOpv1alpha1.SigAddDeploymentOperator{}
	err := r.Get(ctx, req.NamespacedName, SigDepOperator)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If the custom resource is not found then it usually means that it was deleted or not created
			// In this way, we will stop the reconciliation
			log.Info("SigAddDeploymentOperator resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get SigAddDeploymentOperator")
		return ctrl.Result{}, err
	}

	if !SigDepOperator.Spec.Enable {
		log.Info("Operator is disabled, skipping reconciliation")
		return reconcile.Result{}, nil // Return without error as this is intended
	}

	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments); err != nil {
		log.Error(err, "unable to list deployments")
		return reconcile.Result{RequeueAfter: time.Minute}, err
	}
	// Filter deployments containing "sigen" in name
	for _, deployment := range deployments.Items {
		log.Info("Checking deployment", "name", deployment.Name)
		if !strings.Contains(strings.ToLower(deployment.Name), "sigen") {
			continue
		}
		log.Info("Passed deployment", "name", deployment.Name)

		// Get pods for this deployment
		var podList corev1.PodList
		if err := r.List(ctx, &podList, client.MatchingLabels(deployment.Spec.Selector.MatchLabels)); err != nil {
			log.Error(err, "unable to list pods for deployment",
				"deployment", deployment.Name)
			continue
		}

		// Check each pod's metrics
		for _, pod := range podList.Items {
			// Skip pods that aren't running
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}

			// Get pod metrics
			podMetrics, err := r.MetricsClient.MetricsV1beta1().
				PodMetricses(pod.Namespace).
				Get(ctx, pod.Name, metav1.GetOptions{})
			if err != nil {
				log.Error(err, "unable to get pod metrics",
					"namespace", pod.Namespace,
					"pod", pod.Name)
				continue
			}

			// Check each container's memory usage
			for _, container := range podMetrics.Containers {
				memoryQuantity := container.Usage.Memory()
				memoryBytes := float64(memoryQuantity.Value())
				memoryGB := memoryBytes / (1024 * 1024 * 1024) // Convert to GB
				log.Info("Container memory usage",
					"container", container.Name,
					"memory_gb", memoryGB)
				if memoryGB > 1.0 {
					log.Info("Found container using more than 1GB memory",
						"deployment", deployment.Name,
						"namespace", pod.Namespace,
						"pod", pod.Name,
						"container", container.Name,
						"memory_bytes", memoryBytes,
						"memory_GB", memoryGB)

					// Record metrics
					r.podMemoryUsage.WithLabelValues(
						pod.Namespace,
						pod.Name,
					).Set(memoryBytes)

					// CPU metrics for context
					cpuQuantity := container.Usage.Cpu()
					cpuCores := float64(cpuQuantity.MilliValue()) / 1000.0
					r.podCPUUsage.WithLabelValues(
						pod.Namespace,
						pod.Name,
					).Set(cpuCores)
				}
			}
		}
	}
	// Requeue after interval
	return reconcile.Result{RequeueAfter: time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SigAddDeploymentOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&sigDepOpv1alpha1.SigAddDeploymentOperator{}).
		Named("sigadddeploymentoperator").
		Complete(r)
}

// Initialize creates new Prometheus metrics
func (r *SigAddDeploymentOperatorReconciler) Initialize() {
	r.podCPUUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pod_cpu_usage",
			Help: "CPU usage in cores by pod",
		},
		[]string{"namespace", "pod"},
	)

	r.podMemoryUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "pod_memory_usage",
			Help: "Memory usage in bytes by pod",
		},
		[]string{"namespace", "pod"},
	)

	// Register metrics with Prometheus
	prometheus.MustRegister(r.podCPUUsage)
	prometheus.MustRegister(r.podMemoryUsage)
}
