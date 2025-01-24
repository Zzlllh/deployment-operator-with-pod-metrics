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
	"k8s.io/apimachinery/pkg/types"
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

// Add ReplicaSet RBAC permission
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch

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
	currentMemoryThreshold := float64(SigDepOperator.Spec.MemoryThresholdForPod.Value()) / (1024 * 1024 * 1024)
	currentCPUThreshold := float64(SigDepOperator.Spec.CPUThresholdForPod.MilliValue()) / 1000

	currentEMARatio, err := strconv.ParseFloat(SigDepOperator.Spec.EMARatio, 64)
	if err != nil {
		return fmt.Errorf("error parsing EMARatio to float64: %v", err)
	}
	if currentEMARatio < 0 || currentEMARatio > 1 {
		return fmt.Errorf("EMARatio must be between 0 and 1, got %v", currentEMARatio)
	}

	// Get current hour for metrics storage
	currentHour := time.Now().Hour()

	// Check if either threshold has changed
	if currentMemoryThreshold != previousMemoryThreshold ||
		currentCPUThreshold != previousCPUThreshold {
		logger.Info("Thresholds changed, clearing metrics history",
			"old_memory_threshold", previousMemoryThreshold,
			"new_memory_threshold", currentMemoryThreshold,
			"old_cpu_threshold", previousCPUThreshold,
			"new_cpu_threshold", currentCPUThreshold)

		// Clear existing metrics
		sigDepOpv1alpha1.PodUsage = make(map[sigDepOpv1alpha1.PodId]sigDepOpv1alpha1.PodMetrics)

		// Update stored thresholds
		previousMemoryThreshold = currentMemoryThreshold
		previousCPUThreshold = currentCPUThreshold
	}
	// initialize variables on every reconcile
	podSet := make(map[sigDepOpv1alpha1.PodId]struct{})
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

			if err := r.processPods(ctx, log, podList.Items, podSet, currentMemoryThreshold, currentCPUThreshold, currentEMARatio, currentHour); err != nil {
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

			if err := r.processPods(ctx, log, podList.Items, podSet, currentMemoryThreshold, currentCPUThreshold, currentEMARatio, currentHour); err != nil {
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

	for key := range sigDepOpv1alpha1.PodUsage {
		if _, ok := podSet[key]; !ok {
			// k not found in mySet
			delete(sigDepOpv1alpha1.PodUsage, key)
		} else {
			// store the pod usage in the slice
			sigDepOpv1alpha1.KvSliceBasedOnMem = append(sigDepOpv1alpha1.KvSliceBasedOnMem, sigDepOpv1alpha1.IdMetrics{Key: key, Value: sigDepOpv1alpha1.PodUsage[key]})
			sigDepOpv1alpha1.KvSliceBasedOnRatio = append(sigDepOpv1alpha1.KvSliceBasedOnRatio, sigDepOpv1alpha1.IdMetrics{Key: key, Value: sigDepOpv1alpha1.PodUsage[key]})
		}
	}

	wg.Add(2)

	// Sort by memory in parallel
	go func() {
		defer wg.Done()
		sort.Slice(sigDepOpv1alpha1.KvSliceBasedOnMem, func(i, j int) bool {
			return sigDepOpv1alpha1.KvSliceBasedOnMem[i].Value.EMAMemCPURatio > sigDepOpv1alpha1.KvSliceBasedOnMem[j].Value.EMAMemCPURatio
		})
	}()

	// Sort by ratio in parallel
	go func() {
		defer wg.Done()
		sort.Slice(sigDepOpv1alpha1.KvSliceBasedOnRatio, func(i, j int) bool {
			return sigDepOpv1alpha1.KvSliceBasedOnRatio[i].Value.MemCpuRatio[currentHour].Ratio() > sigDepOpv1alpha1.KvSliceBasedOnRatio[j].Value.MemCpuRatio[currentHour].Ratio()
		})
	}()

	// Wait for both sorts to complete
	wg.Wait()

	// Log the KvSliceBasedOnRatio data to verify hourly binning and max values
	for _, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnRatio {
		for hour := 0; hour < 24; hour++ {
			logger.Info("KvSliceBasedOnRatio entry",
				"resource", kvPair.Key.ResourceName,
				"hour", hour,
				"current_ratio", kvPair.Value.MemCpuRatio[hour].Ratio(),
				"current_mem", kvPair.Value.MemCpuRatio[hour].Mem,
				"current_cpu", kvPair.Value.MemCpuRatio[hour].Cpu,
				"max_mem", kvPair.Value.MaxMemory[hour].Mem,
				"max_cpu", kvPair.Value.MaxCPU[hour].Cpu)
		}
	}
	// Create a clean logger for metrics

	// Log the high usage containers with clean output

	// for i, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnMem {
	// 	if i >= SigDepOperator.Spec.DisplayCount {
	// 		break
	// 	}
	// 	logger.Info("Max avg resource usage",
	// 		"current rank", i,
	// 		"name", kvPair.Key.ResourceName,
	// 		"hour", currentHour,
	// 		"MemCpuRatio", kvPair.Value.MemCpuRatio[currentHour].Ratio(),
	// 		"maxmem_Memory", kvPair.Value.MaxMemory[currentHour].Mem,
	// 		"maxcpu_Cpu", kvPair.Value.MaxCPU[currentHour].Cpu,
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

	memSum := 0.0
	cpuSum := 0.0
	placedPods := []sigDepOpv1alpha1.PodId{}
	uniquePodsCount := 0

	// Convert resource.Quantity to float64
	memoryLimit := float64(SigDepOperator.Spec.MemoryLimitForNode.Value()) / (1024 * 1024 * 1024) // Convert to GB
	cpuLimit := float64(SigDepOperator.Spec.CPULimitForNode.MilliValue()) / 1000.0                // Convert millicores to cores

	logger.Info("Resource usage under specified thresholds (sorted by ratio):")
	for _, kvPair := range sigDepOpv1alpha1.KvSliceBasedOnRatio {

		// Check if adding this container would exceed either threshold
		if memSum+kvPair.Value.MaxMemory[currentHour].Mem > memoryLimit || cpuSum+kvPair.Value.MaxCPU[currentHour].Cpu > cpuLimit {
			continue // Skip this container and check next one
		}

		// Add to tracking map and append to slice
		placedPods = append(placedPods, kvPair.Key)
		uniquePodsCount++

		// // Update sums with current hour's data
		// memSum += kvPair.Value.MaxMemory[currentHour].Mem
		// cpuSum += kvPair.Value.MaxCPU[currentHour].Cpu
		// logger.Info("Pod composition from ratio",
		// 	"current rank", i,
		// 	"resource", kvPair.Key.ResourceName,
		// 	"pod", kvPair.Key.PodName,
		// 	"memory_GB", kvPair.Value.MaxMemory[currentHour].Mem,
		// 	"cpu_cores", kvPair.Value.MaxCPU[currentHour].Cpu,
		// )
	}

	// logger.Info("Final totals (ratio-sorted)",
	// 	"memory_limit_GB", memoryLimit,
	// 	"cpu_limit_cores", cpuLimit,
	// 	"final_memory_GB", memSum,
	// 	"final_cpu_cores", cpuSum,
	// 	"pods_counted", uniquePodsCount)

	// Update status with placed pods
	SigDepOperator.Status.PlacedPods = placedPods
	if err := r.Status().Update(ctx, SigDepOperator); err != nil {
		return fmt.Errorf("failed to update SigAddDeploymentOperator status: %v", err)
	}

	return nil
}

// Extract pod processing logic to avoid duplication
func (r *SigAddDeploymentOperatorReconciler) processPods(ctx context.Context, log logr.Logger, pods []corev1.Pod, podSet map[sigDepOpv1alpha1.PodId]struct{}, memoryThreshold, cpuThreshold, emaRatio float64, currentHour int) error {
	for _, pod := range pods {
		// Get resource type and name from owner references
		resourceType, resourceName := "", ""
		if len(pod.OwnerReferences) > 0 {
			owner := pod.OwnerReferences[0]
			if owner.Kind == "ReplicaSet" {
				// Look up the ReplicaSet to find its owner Deployment
				var rs appsv1.ReplicaSet
				if err := r.Get(ctx, types.NamespacedName{Name: owner.Name, Namespace: pod.Namespace}, &rs); err == nil {
					if len(rs.OwnerReferences) > 0 && rs.OwnerReferences[0].Kind == "Deployment" {
						resourceType = "Deployment"
						resourceName = rs.OwnerReferences[0].Name
					}
				}
			} else if owner.Kind == "StatefulSet" {
				resourceType = "StatefulSet"
				resourceName = owner.Name
			}
		}

		// Skip pods that aren't running
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Skip pods younger than 10 minutes
		if time.Since(pod.CreationTimestamp.Time) < 10*time.Minute {
			logger.Info("Skipping young pod",
				"namespace", pod.Namespace,
				"pod", pod.Name,
				"age", time.Since(pod.CreationTimestamp.Time).String(),
				"creation_time", pod.CreationTimestamp.Time)
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

		// Calculate total pod resource usage
		var totalMemoryGB, totalCPUCores float64
		for i, container := range podMetrics.Containers {
			// Get container spec from pod
			if i < len(pod.Spec.Containers) {
				containerSpec := pod.Spec.Containers[i]

				// Memory comparison
				memoryQuantity := container.Usage.Memory()
				memoryBytes := float64(memoryQuantity.Value())
				memoryUsageGB := memoryBytes / (1024 * 1024 * 1024)

				// Get requested memory
				if memoryRequest := containerSpec.Resources.Requests.Memory(); memoryRequest != nil {
					memoryRequestGB := float64(memoryRequest.Value()) / (1024 * 1024 * 1024)
					// Use the larger value
					memoryUsageGB = max(memoryUsageGB, memoryRequestGB)
				}
				totalMemoryGB += memoryUsageGB

				// CPU comparison
				cpuQuantity := container.Usage.Cpu()
				cpuUsageCores := float64(cpuQuantity.MilliValue()) / 1000.0

				// Get requested CPU
				if cpuRequest := containerSpec.Resources.Requests.Cpu(); cpuRequest != nil {
					cpuRequestCores := float64(cpuRequest.MilliValue()) / 1000.0
					// Use the larger value
					cpuUsageCores = max(cpuUsageCores, cpuRequestCores)
				}
				totalCPUCores += cpuUsageCores
			}
		}

		//current Pod Id
		curId := sigDepOpv1alpha1.PodId{
			PodName:      pod.Name,
			Namespace:    pod.Namespace,
			ResourceType: resourceType,
			ResourceName: resourceName,
		}

		// Thread-safe podSet update
		podSetMutex.Lock()
		podSet[curId] = struct{}{}
		podSetMutex.Unlock()

		// Check pod's total resource usage against thresholds
		if totalMemoryGB > memoryThreshold || totalCPUCores > cpuThreshold {
			podMemCpuPair := sigDepOpv1alpha1.MemCpuPair{
				Cpu: totalCPUCores,
				Mem: totalMemoryGB,
			}
			ratio := podMemCpuPair.Ratio()
			//current metrics
			curMetrics := sigDepOpv1alpha1.NewPodMetrics()
			curMetrics.MaxCPU[currentHour] = podMemCpuPair
			curMetrics.MaxMemory[currentHour] = podMemCpuPair
			curMetrics.MemCpuRatio[currentHour] = podMemCpuPair
			curMetrics.EMAMemCPURatio = ratio
			curMetrics.EMAMemory = totalMemoryGB
			curMetrics.EMACpu = totalCPUCores

			// Thread-safe PodUsage update
			containerUsageMutex.Lock()

			if storedMetrics, ok := sigDepOpv1alpha1.PodUsage[curId]; ok {
				storedMetrics.MergeMax(curMetrics)
				storedMetrics.CalculateEMA(curMetrics, emaRatio)
				sigDepOpv1alpha1.PodUsage[curId] = storedMetrics
			} else {
				sigDepOpv1alpha1.PodUsage[curId] = curMetrics
			}
			containerUsageMutex.Unlock()
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
