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
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	infraApi "github.com/opendatahub-io/opendatahub-db-operator/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-db-operator/pkg/controller/claimerrors"
)

func TestDatabaseClaimExtensionsValidator_RejectsUnsupported(t *testing.T) {
	g := NewWithT(t)
	v := &databaseClaimExtensionsValidator{}
	claim := &infraApi.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns"},
		Spec:       infraApi.DatabaseClaimSpec{Extensions: []string{"postgis"}},
	}

	_, err := v.ValidateCreate(context.Background(), claim)
	var unsupported claimerrors.ExtensionNotSupported
	g.Expect(errors.As(err, &unsupported)).To(BeTrue())
	g.Expect(unsupported.Extensions).To(Equal([]string{"postgis"}))
	g.Expect(err).To(MatchError(`extension "postgis" is not supported`))

	_, err = v.ValidateUpdate(context.Background(), claim, claim)
	g.Expect(errors.As(err, &unsupported)).To(BeTrue())
}

func TestDatabaseClaimExtensionsValidator_AllowsSupported(t *testing.T) {
	g := NewWithT(t)
	v := &databaseClaimExtensionsValidator{}
	claim := &infraApi.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns"},
		Spec:       infraApi.DatabaseClaimSpec{Extensions: []string{"vector"}},
	}

	_, err := v.ValidateCreate(context.Background(), claim)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = v.ValidateUpdate(context.Background(), claim, claim)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = v.ValidateDelete(context.Background(), claim)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestSchemaClaimExtensionsValidator_RejectsUnsupported(t *testing.T) {
	g := NewWithT(t)
	v := &schemaClaimExtensionsValidator{}
	claim := &infraApi.SchemaClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns"},
		Spec:       infraApi.SchemaClaimSpec{Extensions: []string{"uuid-ossp"}},
	}

	_, err := v.ValidateCreate(context.Background(), claim)
	var unsupported claimerrors.ExtensionNotSupported
	g.Expect(errors.As(err, &unsupported)).To(BeTrue())
	g.Expect(unsupported.Extensions).To(Equal([]string{"uuid-ossp"}))
	g.Expect(err).To(MatchError(`extension "uuid-ossp" is not supported`))
}

func TestSchemaClaimExtensionsValidator_AllowsSupported(t *testing.T) {
	g := NewWithT(t)
	v := &schemaClaimExtensionsValidator{}
	claim := &infraApi.SchemaClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "ns"},
		Spec:       infraApi.SchemaClaimSpec{Extensions: []string{"vector"}},
	}

	_, err := v.ValidateCreate(context.Background(), claim)
	g.Expect(err).NotTo(HaveOccurred())
}
