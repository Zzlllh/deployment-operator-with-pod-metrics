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
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	metrics "k8s.io/metrics/pkg/client/clientset/versioned"

	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	_ "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"time"

	_ "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	_ "sigs.k8s.io/controller-runtime/pkg/source"

	sigDepOpv1alpha1 "github.com/Zzlllh/deployment-operator-with-pod-metrics/api/v1alpha1"
)

// Previous threshold values for comparison
var (
	previousMemoryThreshold float64
	previousCPUThreshold    float64
)

// SigAddDeploymentOperatorReconciler reconciles a SigAddDeploymentOperator object
type SigAddDeploymentOperatorReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	MetricsClient *metrics.Clientset
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
			log.Info("SigAddDeploymentOperator resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get SigAddDeploymentOperator")
		return ctrl.Result{}, err
	}

	if !SigDepOperator.Spec.Enable {
		log.Info("Operator is disabled, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	if err := r.monitorDeploymentMetrics(ctx, log, SigDepOperator); err != nil {
		log.Error(err, "failed to monitor deployment metrics")
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Requeue after 1 minute for continuous monitoring
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *SigAddDeploymentOperatorReconciler) monitorDeploymentMetrics(ctx context.Context, log logr.Logger, SigDepOperator *sigDepOpv1alpha1.SigAddDeploymentOperator) error {
	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments); err != nil {
		return err
	}

	// Get current thresholds
	currentMemoryThreshold := SigDepOperator.Spec.MemoryThreshold.AsApproximateFloat64()
	currentCPUThreshold := SigDepOperator.Spec.CPUThreshold.AsApproximateFloat64()
	currentEMARatio := SigDepOperator.Spec.EMARatio

	// Check if either threshold has changed
	if currentMemoryThreshold != previousMemoryThreshold ||
		currentCPUThreshold != previousCPUThreshold {
		log.Info("Thresholds changed, clearing metrics history",
			"old_memory_threshold", previousMemoryThreshold,
			"new_memory_threshold", currentMemoryThreshold,

			"old_cpu_threshold", previousCPUThreshold,
			"new_cpu_threshold", currentCPUThreshold)

		// Clear existing metrics
		sigDepOpv1alpha1.ContainerUsage = nil

		// Update stored thresholds
		previousMemoryThreshold = currentMemoryThreshold
		previousCPUThreshold = currentCPUThreshold
	}

	// Filter deployments containing "sigen" in name
	for _, deployment := range deployments.Items {
		// if !strings.Contains(strings.ToLower(deployment.Name), "sigen") {
		// 	continue
		// }
		// Get pods for this deployment
		var podList corev1.PodList
		if err := r.List(ctx, &podList, client.MatchingLabels(deployment.Spec.Selector.MatchLabels)); err != nil {
			log.Error(err, "unable to list pods for deployment", "deployment", deployment.Name)
			continue
		}
		podSet := make(map[sigDepOpv1alpha1.ContainerId]struct{})
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

				cpuQuantity := container.Usage.Cpu()
				cpuCores := float64(cpuQuantity.MilliValue()) / 1000.0

				// Check both memory and CPU thresholds
				if memoryGB > currentMemoryThreshold || cpuCores > currentCPUThreshold {
					//current Id
					curId := sigDepOpv1alpha1.ContainerId{
						ContainerName: container.Name,
						PodName:       pod.Name,
						Namespace:     pod.Namespace,
					}
					//record existing pods to a set
					podSet[curId] = struct{}{}
					ratio := memoryGB / cpuCores
					containerMemCpuPair := sigDepOpv1alpha1.MemCpuPair{
						Cpu: cpuCores,
						Mem: memoryGB,
					}
					//current metrics
					curMetrics := sigDepOpv1alpha1.ContainerMetrics{
						MaxCPU:         containerMemCpuPair,
						MaxMemory:      containerMemCpuPair,
						MemCpuRatio:    containerMemCpuPair,
						EMAMemCPURatio: ratio,
						EMAMemory:      memoryGB,
						EMACpu:         cpuCores,
					}
					//determine if cur pod's metrics is already in, if in, change EMA and determine if its any max usage
					if storedMetrics, ok := sigDepOpv1alpha1.ContainerUsage[curId]; ok {
						storedMetrics.MergeMax(curMetrics)
						storedMetrics.CalculateEMA(curMetrics, currentEMARatio)
						sigDepOpv1alpha1.ContainerUsage[curId] = storedMetrics

					} else {
						sigDepOpv1alpha1.ContainerUsage[curId] = curMetrics
					}
				}
			}
		}
	}

	// Create a clean logger for metrics
	metricsLogger := log.WithName("metrics").V(10)

	// Log the high usage containers with clean output

	for containerID, container := range sigDepOpv1alpha1.ContainerUsage {
		metricsLogger.Info("Resource usage",
			"name", containerID,
			"maxMemCpuRatio", container.MemCpuRatio,
			"maxMemory_mem", container.MaxMemory.Mem,
			"maxMemory_cpu", container.MaxMemory.Cpu,
			"maxCpu", container.MaxCPU,
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SigAddDeploymentOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sigDepOpv1alpha1.SigAddDeploymentOperator{}).
		Named("sigadddeploymentoperator").
		Complete(r)
}
