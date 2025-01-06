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
	"context"
	"fmt"

	cachev1alpha1 "github.com/Zzlllh/deployment-operator-with-pod-metrics/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var sigadddeploymentoperatorlog = logf.Log.WithName("sigadddeploymentoperator-resource")

// SetupSigAddDeploymentOperatorWebhookWithManager registers the webhook for SigAddDeploymentOperator in the manager.
func SetupSigAddDeploymentOperatorWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&cachev1alpha1.SigAddDeploymentOperator{}).
		WithValidator(&SigAddDeploymentOperatorCustomValidator{}).
		WithDefaulter(&SigAddDeploymentOperatorCustomDefaulter{}).
		Complete()
}

func SetupDeploymentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&appsv1.Deployment{}).
		WithDefaulter(&SigAddDeploymentOperatorCustomDefaulter{}). // We already have this defaulter
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// Mutating webhook for deployments
// +kubebuilder:webhook:path=/mutate-apps-v1-deployment,mutating=true,failurePolicy=fail,sideEffects=None,groups=apps,resources=deployments,verbs=create;update,versions=v1,name=mdeployment.kb.io,admissionReviewVersions=v1

// SigAddDeploymentOperatorCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind SigAddDeploymentOperator when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type SigAddDeploymentOperatorCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &SigAddDeploymentOperatorCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind SigAddDeploymentOperator.
func (d *SigAddDeploymentOperatorCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		return fmt.Errorf("expected a Deployment but got %T", obj)
	}
	sigadddeploymentoperatorlog.Info("Defaulting for Deployment", "name", deployment.GetName())

	// Check if deployment name is "test123"
	if deployment.GetName() == "test123" {
		// Set replicas to 2
		replicas := int32(2)
		deployment.Spec.Replicas = &replicas

		// Add annotation
		if deployment.Annotations == nil {
			deployment.Annotations = make(map[string]string)
		}
		deployment.Annotations["test"] = "working"

		// Add node affinity
		if deployment.Spec.Template.Spec.Affinity == nil {
			deployment.Spec.Template.Spec.Affinity = &corev1.Affinity{}
		}

		deployment.Spec.Template.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "memory",
								Operator: corev1.NodeSelectorOpIn,
								Values:   []string{"high"},
							},
						},
					},
				},
			},
		}
	}

	return nil
}

// Validation webhook for our CRD
// +kubebuilder:webhook:path=/validate-cache-sig-com-v1alpha1-sigadddeploymentoperator,mutating=false,failurePolicy=fail,sideEffects=None,groups=cache.sig.com,resources=sigadddeploymentoperators,verbs=create;update,versions=v1alpha1,name=vsigadddeploymentoperator-v1alpha1.kb.io,admissionReviewVersions=v1

// SigAddDeploymentOperatorCustomValidator struct is responsible for validating the SigAddDeploymentOperator resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type SigAddDeploymentOperatorCustomValidator struct {
	//TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &SigAddDeploymentOperatorCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type SigAddDeploymentOperator.
func (v *SigAddDeploymentOperatorCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	sigadddeploymentoperator, ok := obj.(*cachev1alpha1.SigAddDeploymentOperator)
	if !ok {
		return nil, fmt.Errorf("expected a SigAddDeploymentOperator object but got %T", obj)
	}
	sigadddeploymentoperatorlog.Info("Validation for SigAddDeploymentOperator upon creation", "name", sigadddeploymentoperator.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type SigAddDeploymentOperator.
func (v *SigAddDeploymentOperatorCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	sigadddeploymentoperator, ok := newObj.(*cachev1alpha1.SigAddDeploymentOperator)
	if !ok {
		return nil, fmt.Errorf("expected a SigAddDeploymentOperator object for the newObj but got %T", newObj)
	}
	sigadddeploymentoperatorlog.Info("Validation for SigAddDeploymentOperator upon update", "name", sigadddeploymentoperator.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type SigAddDeploymentOperator.
func (v *SigAddDeploymentOperatorCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	sigadddeploymentoperator, ok := obj.(*cachev1alpha1.SigAddDeploymentOperator)
	if !ok {
		return nil, fmt.Errorf("expected a SigAddDeploymentOperator object but got %T", obj)
	}
	sigadddeploymentoperatorlog.Info("Validation for SigAddDeploymentOperator upon deletion", "name", sigadddeploymentoperator.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
