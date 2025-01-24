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

package v1alpha1

import (
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SigAddDeploymentOperatorSpec defines the desired state of SigAddDeploymentOperator.
type SigAddDeploymentOperatorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Enable bool `json:"enable"`
	// MemoryThreshold specifies the memory threshold value that triggers the operator
	MemoryThresholdForPod resource.Quantity `json:"memoryThresholdForPod"`
	CPUThresholdForPod    resource.Quantity `json:"cpuThresholdForPod"`
	//ratio for Exponential Moving Average to calculate an approx avg
	EMARatio     string `json:"emaRatio"`
	DisplayCount int    `json:"displayCount"`
	// MemoryLimit specifies the memory threshold for pod placement
	// +kubebuilder:validation:Required
	MemoryLimitForNode resource.Quantity `json:"memoryLimitForNode"`

	// CPULimit specifies the CPU threshold for pod placement
	// +kubebuilder:validation:Required
	CPULimitForNode resource.Quantity `json:"cpuLimitForNode"`
}

// SigAddDeploymentOperatorStatus defines the observed state of SigAddDeploymentOperator.
type SigAddDeploymentOperatorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	LastUpdated metav1.Time        `json:"lastUpdated,omitempty"`
	// PlacedPods tracks the pods that have been placed
	// +optional
	PlacedPods []PodId `json:"placedPods,omitempty"`
	// ActivationTime tracks when the operator was enabled
	// +optional
	ActivationTime *metav1.Time `json:"activationTime,omitempty"`
}

type IdMetrics struct {
	Key   PodId
	Value PodMetrics
}

// PodMetrics holds the metrics information for a pod
type PodMetrics struct {
	// Using slices instead of fixed-size arrays
	MaxCPU      []MemCpuPair `json:"maxCPUUsage"`
	MaxMemory   []MemCpuPair `json:"maxMemoryUsage"`
	MemCpuRatio []MemCpuPair `json:"maxMemCpuRatio"`

	// Exponential Moving Average fields
	EMAMemCPURatio float64 `json:"emaMCR"`
	EMAMemory      float64 `json:"emaMem"`
	EMACpu         float64 `json:"emaCPU"`
}

// Helper method to get current hour's index (0-23)
func getCurrentHourIndex() int {
	return time.Now().Hour()
}

// Update MergeMax to handle hourly metrics
func (m *PodMetrics) MergeMax(other PodMetrics) {
	hour := getCurrentHourIndex()

	if other.MemCpuRatio[hour].Ratio() > m.MemCpuRatio[hour].Ratio() {
		m.MemCpuRatio[hour] = other.MemCpuRatio[hour]
	}
	if other.MaxMemory[hour].Mem > m.MaxMemory[hour].Mem {
		m.MaxMemory[hour] = other.MaxMemory[hour]
	}
	if other.MaxCPU[hour].Cpu > m.MaxCPU[hour].Cpu {
		m.MaxCPU[hour] = other.MaxCPU[hour]
	}
}

// Update CalculateEMA to use current hour's metrics
func (m *PodMetrics) CalculateEMA(other PodMetrics, ratio float64) {
	hour := getCurrentHourIndex()

	m.EMACpu = other.MaxCPU[hour].Cpu*ratio + (1.0-ratio)*m.EMACpu
	m.EMAMemory = other.MaxMemory[hour].Mem*ratio + (1.0-ratio)*m.EMAMemory
	m.EMAMemCPURatio = other.MemCpuRatio[hour].Ratio()*float64(ratio) + (1.0-ratio)*m.EMAMemCPURatio
}

type PodId struct {
	PodName      string `json:"podName"`
	Namespace    string `json:"namespace"`
	ResourceName string `json:"resourceName"`
	ResourceType string `json:"resourceType"`
}

type MemCpuPair struct {
	Cpu float64 `json:"cpuUsage"`
	Mem float64 `json:"memoryUsage"`
}

func (pair MemCpuPair) Ratio() float64 {
	return pair.Mem / (pair.Cpu + 0.001) //add 0.001 to avoid divide by 0
}

// PodUsage stores pods usage
var PodUsage = make(map[PodId]PodMetrics)

// key value slice based on different metrics
var KvSliceBasedOnMem []IdMetrics
var KvSliceBasedOnRatio []IdMetrics

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// SigAddDeploymentOperator is the Schema for the sigadddeploymentoperators API.
type SigAddDeploymentOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SigAddDeploymentOperatorSpec   `json:"spec,omitempty"`
	Status SigAddDeploymentOperatorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SigAddDeploymentOperatorList contains a list of SigAddDeploymentOperator.
type SigAddDeploymentOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SigAddDeploymentOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SigAddDeploymentOperator{}, &SigAddDeploymentOperatorList{})
}

// NewPodMetrics creates a new PodMetrics instance with initialized slices
func NewPodMetrics() PodMetrics {
	return PodMetrics{
		MaxCPU:      make([]MemCpuPair, 24), // Initialize with 24 zero values
		MaxMemory:   make([]MemCpuPair, 24), // Initialize with 24 zero values
		MemCpuRatio: make([]MemCpuPair, 24), // Initialize with 24 zero values
		// EMA fields will automatically be initialized to 0
	}
}
