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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Define condition types
const (
	ConditionEnabled = "Enabled"
	ConditionStopped = "Stopped"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
func setCondition(status *SigAddDeploymentOperatorStatus, conditionType, statusValue, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionStatus(statusValue),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	// Check if the condition already exists
	for i, cond := range status.Conditions {
		if cond.Type == conditionType {
			status.Conditions[i] = condition
			return
		}
	}

	// If not, append the new condition
	status.Conditions = append(status.Conditions, condition)
}

// SigAddDeploymentOperatorSpec defines the desired state of SigAddDeploymentOperator.
type SigAddDeploymentOperatorSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Enable          bool   `json:"enable,omitempty"`
	MemoryThreshold string `json:"memoryThreshold,omitempty"`
}

// SigAddDeploymentOperatorStatus defines the observed state of SigAddDeploymentOperator.
type SigAddDeploymentOperatorStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Conditions  []metav1.Condition `json:"conditions,omitempty"`
	LastUpdated metav1.Time        `json:"lastUpdated,omitempty"`
}

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
