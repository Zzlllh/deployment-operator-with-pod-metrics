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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SigAddDeploymentOperatorSpec defines the desired state of SigAddDeploymentOperator.
type SigAddDeploymentOperatorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Enable bool `json:"enable"`
	// MemoryThreshold specifies the memory threshold value that triggers the operator
	MemoryThreshold resource.Quantity `json:"memoryThreshold"`
	CPUThreshold    resource.Quantity `json:"cpuThreshold"`
	//ratio for Exponential Moving Average to calculate an approx avg
	EMARatio     string `json:"emaRatio"`
	DisplayCount int    `json:"displayCount"`
}

// SigAddDeploymentOperatorStatus defines the observed state of SigAddDeploymentOperator.
type SigAddDeploymentOperatorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	LastUpdated metav1.Time        `json:"lastUpdated,omitempty"`
}

type IdMetrics struct {
	Key   ContainerId
	Value ContainerMetrics
}

// ContainerMetrics holds the metrics information for a container
// Note: This is a helper type and not registered as a Kubernetes API type
type ContainerMetrics struct {
	MaxCPU      MemCpuPair `json:"maxCPUUsage"`
	MaxMemory   MemCpuPair `json:"maxMemoryUsage"`
	MemCpuRatio MemCpuPair `json:"maxMemCpuRatio"`
	//Exponential Moving Average to calculate an approx avg
	EMAMemCPURatio float64 `json:"emaMCR"` // memory cpu ratio
	EMAMemory      float64 `json:"emaMem"`
	EMACpu         float64 `json:"emaCPU"`
}

func (m *ContainerMetrics) MergeMax(other ContainerMetrics) {

	if other.MemCpuRatio.Ratio() > m.MemCpuRatio.Ratio() {
		m.MemCpuRatio = other.MemCpuRatio
	}
	if other.MaxMemory.Mem > m.MaxMemory.Mem {
		m.MaxMemory.Mem = other.MaxMemory.Mem
	}
	if other.MaxCPU.Cpu > m.MaxCPU.Cpu {
		m.MaxCPU.Cpu = other.MaxCPU.Cpu
	}
}
func (m *ContainerMetrics) CalculateEMA(other ContainerMetrics, ratio float64) {
	m.EMACpu = other.EMACpu*ratio + (1.0-ratio)*m.EMACpu
	m.EMAMemory = other.EMAMemory*ratio + (1.0-ratio)*m.EMAMemory
	m.EMAMemCPURatio = other.MemCpuRatio.Ratio()*float64(ratio) + (1.0-ratio)*m.EMAMemCPURatio
}

type ContainerId struct {
	ContainerName string `json:"containerName"`
	PodName       string `json:"podName"`
	Namespace     string `json:"namespace"`
}

type MemCpuPair struct {
	Cpu float64 `json:"cpuUsage"`
	Mem float64 `json:"memoryUsage"`
}

func (pair MemCpuPair) Ratio() float64 {
	return pair.Mem / (pair.Cpu + 0.001) //add 0.001 to avoid divide by 0
}

// containerUsage stores containers usage
var ContainerUsage = make(map[ContainerId]ContainerMetrics)

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
