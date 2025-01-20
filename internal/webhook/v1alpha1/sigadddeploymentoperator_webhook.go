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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// nolint:unused
// log is for logging in this package.
var sigadddeploymentoperatorlog = logf.Log.WithName("sigadddeploymentoperator-resource")

// SetupSigAddDeploymentOperatorWebhookWithManager registers the webhook for SigAddDeploymentOperator in the manager.
func SetupSigAddDeploymentOperatorWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&cachev1alpha1.SigAddDeploymentOperator{}).
		WithValidator(&SigAddDeploymentOperatorCustomValidator{
			Client: mgr.GetClient(),
		}).
		WithDefaulter(&SigAddDeploymentOperatorCustomDefaulter{}).
		Complete()
}

func SetupWebhookWithManager(mgr ctrl.Manager) error {
	defaulter := &SigAddDeploymentOperatorCustomDefaulter{
		Client: mgr.GetClient(),
	}

	// Setup webhook for Deployment
	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&appsv1.Deployment{}).
		WithDefaulter(defaulter).
		Complete(); err != nil {
		return err
	}

	// Setup webhook for StatefulSet
	if err := ctrl.NewWebhookManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		WithDefaulter(defaulter).
		Complete(); err != nil {
		return err
	}

	return nil
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// Mutating webhook for deployments
// +kubebuilder:webhook:path=/mutate-apps-v1-deployment,mutating=true,failurePolicy=fail,sideEffects=None,groups=apps,resources=deployments,verbs=create;update,versions=v1,name=mdeployment.kb.io,admissionReviewVersions=v1

// SigAddDeploymentOperatorCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind SigAddDeploymentOperator when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
// +kubebuilder:rbac:groups=cache.sig.com,resources=sigadddeploymentoperators,verbs=get;list;watch
type SigAddDeploymentOperatorCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &SigAddDeploymentOperatorCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind SigAddDeploymentOperator.
func (r *SigAddDeploymentOperatorCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	// Get the resource being mutated
	var resourceMeta metav1.Object
	var resourceKind string

	switch v := obj.(type) {
	case *appsv1.Deployment:
		resourceMeta = v.GetObjectMeta()
		resourceKind = "Deployment"
	case *appsv1.StatefulSet:
		resourceMeta = v.GetObjectMeta()
		resourceKind = "StatefulSet"
	default:
		return nil
	}

	log := ctrl.Log.WithName("webhook")

	// Get the single SigAddDeploymentOperator instance
	sigDepOpList := &cachev1alpha1.SigAddDeploymentOperatorList{}
	if err := r.Client.List(ctx, sigDepOpList, client.Limit(1)); err != nil {
		log.Error(err, "Failed to list SigAddDeploymentOperator")
		// If we can't get the CR, skip mutation
		return nil
	}

	// Since we enforce single instance via validation webhook,
	// we can safely check the first (and only) item
	if len(sigDepOpList.Items) == 0 {
		log.Info("No SigAddDeploymentOperator instance found")
		return nil
	}

	sigDepOp := &sigDepOpList.Items[0]
	// Check if operator is enabled
	if !sigDepOp.Spec.Enable {
		return nil
	}

	// Check if this resource is in PlacedPods
	for _, placedPod := range sigDepOp.Status.PlacedPods {
		if placedPod.Namespace == resourceMeta.GetNamespace() &&
			placedPod.ResourceName == resourceMeta.GetName() &&
			placedPod.ResourceType == resourceKind {
			log.Info("PlacedPod found", "namespace", placedPod.Namespace, "resourceName", placedPod.ResourceName, "resourceType", placedPod.ResourceType)

			// Add annotation
			annotations := resourceMeta.GetAnnotations()
			if annotations == nil {
				annotations = make(map[string]string)
			}

			// Add annotation indicating this resource is managed by the operator
			annotations["metrics-dep-operator/managed"] = "true"
			// Add annotation with container name
			annotations["metrics-dep-operator/container"] = placedPod.ContainerName

			resourceMeta.SetAnnotations(annotations)
			break
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
	Client client.Client
}

var _ webhook.CustomValidator = &SigAddDeploymentOperatorCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type SigAddDeploymentOperator.
func (v *SigAddDeploymentOperatorCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	sigadddeploymentoperator, ok := obj.(*cachev1alpha1.SigAddDeploymentOperator)
	if !ok {
		return nil, fmt.Errorf("expected a SigAddDeploymentOperator object but got %T", obj)
	}
	sigadddeploymentoperatorlog.Info("Validation for SigAddDeploymentOperator upon creation", "name", sigadddeploymentoperator.GetName())

	// Get the list of existing SigAddDeploymentOperator instances
	existingList := &cachev1alpha1.SigAddDeploymentOperatorList{}
	if err := v.Client.List(ctx, existingList); err != nil {
		return nil, fmt.Errorf("failed to check existing instances: %v", err)
	}

	// If any instances exist, reject the creation
	if len(existingList.Items) > 0 {
		return nil, fmt.Errorf("only one instance of SigAddDeploymentOperator is allowed, found existing instance: %s", existingList.Items[0].Name)
	}

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
