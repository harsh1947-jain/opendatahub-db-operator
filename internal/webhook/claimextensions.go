/*
Copyright 2026.

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

package webhook

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	infraApi "github.com/opendatahub-io/opendatahub-db-operator/api/infrastructure/v1alpha1"
	dbcontroller "github.com/opendatahub-io/opendatahub-db-operator/pkg/controller"
)

const (
	databaseClaimValidatePath = "/validate-infrastructure-opendatahub-io-v1alpha1-databaseclaim"
	schemaClaimValidatePath   = "/validate-infrastructure-opendatahub-io-v1alpha1-schemaclaim"
)

// +kubebuilder:webhook:path=/validate-infrastructure-opendatahub-io-v1alpha1-databaseclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.opendatahub.io,resources=databaseclaims,verbs=create;update,versions=v1alpha1,name=vdcextensions.infrastructure.opendatahub.io,admissionReviewVersions=v1,timeoutSeconds=10
// +kubebuilder:webhook:path=/validate-infrastructure-opendatahub-io-v1alpha1-schemaclaim,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.opendatahub.io,resources=schemaclaims,verbs=create;update,versions=v1alpha1,name=vscextensions.infrastructure.opendatahub.io,admissionReviewVersions=v1,timeoutSeconds=10

// SetupClaimExtensions registers validating webhooks that reject claim
// spec.extensions outside SupportedClaimExtensions.
func SetupClaimExtensions(mgr ctrl.Manager, registerDatabaseClaim, registerSchemaClaim bool) error {
	if registerDatabaseClaim {
		if err := ctrl.NewWebhookManagedBy(mgr, &infraApi.DatabaseClaim{}).
			WithValidator(&databaseClaimExtensionsValidator{}).
			WithValidatorCustomPath(databaseClaimValidatePath).
			Complete(); err != nil {
			return fmt.Errorf("registering DatabaseClaim extensions webhook: %w", err)
		}
	}
	if registerSchemaClaim {
		if err := ctrl.NewWebhookManagedBy(mgr, &infraApi.SchemaClaim{}).
			WithValidator(&schemaClaimExtensionsValidator{}).
			WithValidatorCustomPath(schemaClaimValidatePath).
			Complete(); err != nil {
			return fmt.Errorf("registering SchemaClaim extensions webhook: %w", err)
		}
	}
	return nil
}

type databaseClaimExtensionsValidator struct{}

func (v *databaseClaimExtensionsValidator) ValidateCreate(_ context.Context, obj *infraApi.DatabaseClaim) (admission.Warnings, error) {
	return nil, dbcontroller.ValidateClaimExtensions(obj.Spec.Extensions)
}

func (v *databaseClaimExtensionsValidator) ValidateUpdate(_ context.Context, _, newObj *infraApi.DatabaseClaim) (admission.Warnings, error) {
	return nil, dbcontroller.ValidateClaimExtensions(newObj.Spec.Extensions)
}

func (v *databaseClaimExtensionsValidator) ValidateDelete(context.Context, *infraApi.DatabaseClaim) (admission.Warnings, error) {
	return nil, nil
}

type schemaClaimExtensionsValidator struct{}

func (v *schemaClaimExtensionsValidator) ValidateCreate(_ context.Context, obj *infraApi.SchemaClaim) (admission.Warnings, error) {
	return nil, dbcontroller.ValidateClaimExtensions(obj.Spec.Extensions)
}

func (v *schemaClaimExtensionsValidator) ValidateUpdate(_ context.Context, _, newObj *infraApi.SchemaClaim) (admission.Warnings, error) {
	return nil, dbcontroller.ValidateClaimExtensions(newObj.Spec.Extensions)
}

func (v *schemaClaimExtensionsValidator) ValidateDelete(context.Context, *infraApi.SchemaClaim) (admission.Warnings, error) {
	return nil, nil
}
