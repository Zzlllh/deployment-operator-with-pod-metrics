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
	"math"
	"sort"
	"strconv"
	"sync"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

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
	MaxHeap                 []int
	containerUsageMutex     sync.Mutex
	podSetMutex             sync.Mutex
)
var logger = zap.New(zap.UseDevMode(true)).WithName("logger")

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

// Add StatefulSet RBAC permission
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch

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

	if err := r.monitorWorkloadMetrics(ctx, log, SigDepOperator); err != nil {
		log.Error(err, "failed to monitor deployment metrics")
	}

	// Requeue after 1 minute for continuous monitoring
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// Rename function to reflect expanded scope
func (r *SigAddDeploymentOperatorReconciler) monitorWorkloadMetrics(ctx context.Context, log logr.Logger, SigDepOperator *sigDepOpv1alpha1.SigAddDeploymentOperator) error {
	// List Deployments
	var deployments appsv1.DeploymentList
	if err := r.List(ctx, &deployments); err != nil {
		return err
	}

	// Add StatefulSet listing
	var statefulsets appsv1.StatefulSetList
	if err := r.List(ctx, &statefulsets); err != nil {
		return err
	}

	// Get current thresholds
	currentMemoryThreshold := SigDepOperator.Spec.MemoryThreshold.AsApproximateFloat64() / 1000 / 1000 / 1000
	currentCPUThreshold := SigDepOperator.Spec.CPUThreshold.AsApproximateFloat64()

	currentEMARatio, err := strconv.ParseFloat(SigDepOperator.Spec.EMARatio, 64)
	if err != nil || math.Abs(currentEMARatio) >= 1 {
		// Handle parsing error
		fmt.Println("Error parsing string to float64:", err)
		return err
	}

	// Check if either threshold has changed
	if currentMemoryThreshold != previousMemoryThreshold ||
		currentCPUThreshold != previousCPUThreshold {
		logger.Info("Thresholds changed, clearing metrics history",
			"old_memory_threshold", previousMemoryThreshold,
			"new_memory_threshold", currentMemoryThreshold,
			"old_cpu_threshold", previousCPUThreshold,
			"new_cpu_threshold", currentCPUThreshold)

		// Clear existing metrics
		sigDepOpv1alpha1.ContainerUsage = make(map[sigDepOpv1alpha1.ContainerId]sigDepOpv1alpha1.ContainerMetrics)

		// Update stored thresholds
		previousMemoryThreshold = currentMemoryThreshold
		previousCPUThreshold = currentCPUThreshold
	}
	// initialize variables on every reconcile
	podSet := make(map[sigDepOpv1alpha1.ContainerId]struct{})
	sigDepOpv1alpha1.KvSliceBasedOnMem = nil
	sigDepOpv1alpha1.KvSliceBasedOnRatio = nil

	var wg sync.WaitGroup
	errChan := make(chan error, len(deployments.Items)+len(statefulsets.Items))

	// Process Deployments in parallel
	for _, deployment := range deployments.Items {
		wg.Add(1)
		go func(dep appsv1.Deployment) {
			defer wg.Done()

			var podList corev1.PodList
			if err := r.List(ctx, &podList, client.MatchingLabels(dep.Spec.Selector.MatchLabels)); err != nil {
				log.Error(err, "unable to list pods for deployment", "deployment", dep.Name)
				errChan <- err
				return
			}

			if err := r.processPods(ctx, log, podList.Items, podSet, currentMemoryThreshold, currentCPUThreshold, currentEMARatio); err != nil {
				log.Error(err, "error processing deployment pods")
				errChan <- err
			}
		}(deployment)
	}

	// Process StatefulSets in parallel
	for _, statefulset := range statefulsets.Items {
		wg.Add(1)
		go func(sts appsv1.StatefulSet) {
			defer wg.Done()

			var podList corev1.PodList
			if err := r.List(ctx, &podList, client.MatchingLabels(sts.Spec.Selector.MatchLabels)); err != nil {
				log.Error(err, "unable to list pods for statefulset", "statefulset", sts.Name)
				errChan <- err
				return
			}

			if err := r.processPods(ctx, log, podList.Items, podSet, currentMemoryThreshold, currentCPUThreshold, currentEMARatio); err != nil {
				log.Error(err, "error processing statefulset pods")
				errChan <- err
			}
		}(statefulset)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Collect any errors
	var errs []error
	for err := range errChan {
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		logger.Error(fmt.Errorf("encountered %d errors during processing: %v", len(errs), errs), "errors occurred while processing pods")
	}

	for key := range sigDepOpv1alpha1.ContainerUsage {
		if _, ok := podSet[key]; !ok {
			// k not found in mySet
			delete(sigDepOpv1alpha1.ContainerUsage, key)
		} else {
			sigDepOpv1alpha1.KvSliceBasedOnMem = append(sigDepOpv1alpha1.KvSliceBasedOnMem, sigDepOpv1alpha1.IdMetrics{Key: key, Value: sigDepOpv1alpha1.ContainerUsage[key]})
			sigDepOpv1alpha1.KvSliceBasedOnRatio = append(sigDepOpv1alpha1.KvSliceBasedOnRatio, sigDepOpv1alpha1.IdMetrics{Key: key, Value: sigDepOpv1alpha1.ContainerUsage[key]})
		}
	}

	wg.Add(2)

	// Sort by memory in parallel
	go func() {
		defer wg.Done()
		sort.Slice(sigDepOpv1alpha1.KvSliceBasedOnMem, func(i, j int) bool {
			return sigDepOpv1alpha1.KvSliceBasedOnMem[i].Value.MaxMemory.Mem > sigDepOpv1alpha1.KvSliceBasedOnMem[j].Value.MaxMemory.Mem
		})
	}()

	// Sort by ratio in parallel
	go func() {
		defer wg.Done()
		sort.Slice(sigDepOpv1alpha1.KvSliceBasedOnRatio, func(i, j int) bool {
			return sigDepOpv1alpha1.KvSliceBasedOnRatio[i].Value.MemCpuRatio.Ratio() > sigDepOpv1alpha1.KvSliceBasedOnRatio[j].Value.MemCpuRatio.Ratio()
		})
	}()

	// Wait for both sorts to complete
	wg.Wait()

	// Create a clean logger for metrics

	// Log the high usage containers with clean output

	// for i, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnMem {
	// 	if i >= SigDepOperator.Spec.DisplayCount {
	// 		break
	// 	}
	// 	logger.Info("Max memory resource usage",
	// 		"current rank", i,
	// 		"name", kvPair.Key.ContainerName,
	// 		"MemCpuRatio", kvPair.Value.MemCpuRatio.Ratio(),
	// 		"maxmem_Memory", kvPair.Value.MaxMemory.Mem,
	// 		"maxmem_Cpu", kvPair.Value.MaxMemory.Cpu,
	// 		"maxcpu_Memory", kvPair.Value.MaxCPU.Mem,
	// 		"maxcpu_Cpu", kvPair.Value.MaxCPU.Cpu,
	// 		"EMA", kvPair.Value.EMAMemCPURatio,
	// 	)
	// }
	// for i, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnRatio {
	// 	if i >= SigDepOperator.Spec.DisplayCount {
	// 		break
	// 	}
	// 	logger.Info("Max ratio resource usage",
	// 		"current rank", i,
	// 		"name", kvPair.Key.ContainerName,
	// 		"MemCpuRatio", kvPair.Value.MemCpuRatio.Ratio(),
	// 		"maxmem_Memory", kvPair.Value.MaxMemory.Mem,
	// 		"maxmem_Cpu", kvPair.Value.MaxMemory.Cpu,
	// 		"maxcpu_Memory", kvPair.Value.MaxCPU.Mem,
	// 		"maxcpu_Cpu", kvPair.Value.MaxCPU.Cpu,
	// 		"EMA", kvPair.Value.EMAMemCPURatio,
	// 	)
	// }

	// For memory-sorted slice
	memSum := 0.0
	cpuSum := 0.0
	sigDepOpv1alpha1.PlacedPods = make(map[string]struct{})
	uniquePodsCount := 0
	logger.Info("Resource usage under 60GB memory and 7.5 CPU (sorted by max memory):")
	for i, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnMem {
		// Skip if container name already seen
		if _, exists := sigDepOpv1alpha1.PlacedPods[kvPair.Key.ContainerName]; exists {
			continue
		}

		// Check if adding this container would exceed either threshold
		if memSum+kvPair.Value.MaxMemory.Mem > 58.0 || cpuSum+kvPair.Value.MaxCPU.Cpu > 7.5 {
			continue // Skip this container and check next one
		}

		sigDepOpv1alpha1.PlacedPods[kvPair.Key.ContainerName] = struct{}{}
		uniquePodsCount++

		memSum += kvPair.Value.MaxMemory.Mem
		cpuSum += kvPair.Value.MaxCPU.Cpu
		logger.Info("Pod composition from mem",
			"current rank", i,
			"container", kvPair.Key.ContainerName,
			"pod", kvPair.Key.PodName,
			"memory_GB", kvPair.Value.MaxMemory.Mem,
			"cpu_cores", kvPair.Value.MaxCPU.Cpu,
		)
	}
	logger.Info("Final totals (memory-sorted)",
		"final_memory_GB", memSum,
		"final_cpu_cores", cpuSum,
		"pods_counted", uniquePodsCount)

	// For ratio-sorted slice
	memSum = 0.0
	cpuSum = 0.0
	sigDepOpv1alpha1.PlacedPods = make(map[string]struct{})
	uniquePodsCount = 0
	logger.Info("Resource usage under 60GB memory and 7.5 CPU (sorted by ratio):")
	for i, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnRatio {
		// Skip if container name already seen
		if _, exists := sigDepOpv1alpha1.PlacedPods[kvPair.Key.ContainerName]; exists {
			continue
		}

		// Check if adding this container would exceed either threshold
		if memSum+kvPair.Value.MaxMemory.Mem > 58.0 || cpuSum+kvPair.Value.MaxCPU.Cpu > 7.5 {
			continue // Skip this container and check next one
		}

		sigDepOpv1alpha1.PlacedPods[kvPair.Key.ContainerName] = struct{}{}
		uniquePodsCount++

		memSum += kvPair.Value.MaxMemory.Mem
		cpuSum += kvPair.Value.MaxCPU.Cpu
		logger.Info("Pod composition from ratio",
			"current rank", i,
			"container", kvPair.Key.ContainerName,
			"pod", kvPair.Key.PodName,
			"memory_GB", kvPair.Value.MaxMemory.Mem,
			"cpu_cores", kvPair.Value.MaxCPU.Cpu,
			"running_memory_total", memSum,
			"running_cpu_total", cpuSum,
		)
	}
	logger.Info("Final totals (ratio-sorted)",
		"final_memory_GB", memSum,
		"final_cpu_cores", cpuSum,
		"pods_counted", uniquePodsCount)
	return nil
}

// Extract pod processing logic to avoid duplication
func (r *SigAddDeploymentOperatorReconciler) processPods(ctx context.Context, log logr.Logger, pods []corev1.Pod, podSet map[sigDepOpv1alpha1.ContainerId]struct{}, memoryThreshold, cpuThreshold, emaRatio float64) error {
	for _, pod := range pods {
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
			memoryGB := memoryBytes / (1024 * 1024 * 1024)

			cpuQuantity := container.Usage.Cpu()
			cpuCores := float64(cpuQuantity.MilliValue()) / 1000.0

			//current Id
			curId := sigDepOpv1alpha1.ContainerId{
				ContainerName: container.Name,
				PodName:       pod.Name,
				Namespace:     pod.Namespace,
			}

			// Thread-safe podSet update
			podSetMutex.Lock()
			podSet[curId] = struct{}{}
			podSetMutex.Unlock()

			// Check both memory and CPU thresholds
			if memoryGB > memoryThreshold || cpuCores > cpuThreshold {
				containerMemCpuPair := sigDepOpv1alpha1.MemCpuPair{
					Cpu: cpuCores,
					Mem: memoryGB,
				}
				ratio := containerMemCpuPair.Ratio()
				//current metrics
				curMetrics := sigDepOpv1alpha1.ContainerMetrics{
					MaxCPU:         containerMemCpuPair,
					MaxMemory:      containerMemCpuPair,
					MemCpuRatio:    containerMemCpuPair,
					EMAMemCPURatio: ratio,
					EMAMemory:      memoryGB,
					EMACpu:         cpuCores,
				}

				// Thread-safe ContainerUsage update
				containerUsageMutex.Lock()
				if storedMetrics, ok := sigDepOpv1alpha1.ContainerUsage[curId]; ok {
					storedMetrics.MergeMax(curMetrics)
					storedMetrics.CalculateEMA(curMetrics, emaRatio)
					sigDepOpv1alpha1.ContainerUsage[curId] = storedMetrics
				} else {
					sigDepOpv1alpha1.ContainerUsage[curId] = curMetrics
				}
				containerUsageMutex.Unlock()
			}
		}
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
