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

package databaseclaim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/gomega"
	fwapi "github.com/opendatahub-io/odh-platform-utilities/framework/api"
	odherrors "github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions/errors"
	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/conditions"
	odhtypes "github.com/opendatahub-io/odh-platform-utilities/framework/controller/types"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	infraApi "github.com/opendatahub-io/opendatahub-db-operator/api/infrastructure/v1alpha1"
	moduleconfig "github.com/opendatahub-io/opendatahub-db-operator/pkg/config"
	"github.com/opendatahub-io/opendatahub-db-operator/pkg/controller/claimerrors"
	"github.com/opendatahub-io/opendatahub-db-operator/pkg/postgres"
)

func TestProvisionAction_PersistsProviderBindingBeforeEnsure(t *testing.T) {
	g := NewWithT(t)

	provider := &infraApi.DatabaseProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "rhai-db"},
		Spec: infraApi.DatabaseProviderSpec{
			Type: infraApi.ProviderTypeInternal,
			Internal: &infraApi.InternalProviderSpec{
				Namespace: "database-system",
			},
		},
		Status: infraApi.DatabaseProviderStatus{
			Status: fwapi.Status{
				Conditions: []fwapi.Condition{{
					Type:   "Reachable",
					Status: metav1.ConditionTrue,
				}},
			},
		},
	}
	claim := &infraApi.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "analytics", Namespace: "app"},
		Spec: infraApi.DatabaseClaimSpec{
			Provider: infraApi.ProviderRef{Name: "rhai-db"},
			Database: "analytics",
		},
	}

	cli := fake.NewClientBuilder().WithScheme(claimTestScheme(t)).WithObjects(provider, claim).Build()
	rr := claimTestRequest(t, cli, claim)
	m := &Controller{Options: Options{cfg: &moduleconfig.Config{}}}

	err := m.provisionAction(context.Background(), rr)
	g.Expect(err).To(HaveOccurred())
	var stop odherrors.StopError
	g.Expect(errors.As(err, &stop)).To(BeTrue())
	g.Expect(stop.RequeueAfter()).To(Equal(providerBindingRequeueAfter))
	g.Expect(claim.Status.Provider).To(Equal("rhai-db"))
	provisioned := conditions.FindStatusCondition(claim, ConditionProvisioned)
	g.Expect(provisioned).NotTo(BeNil())
	g.Expect(provisioned.Reason).To(Equal("ProviderBound"))
}

func TestProvisionAction_ExtensionNotAvailable(t *testing.T) {
	g := NewWithT(t)

	provider := &infraApi.DatabaseProvider{
		ObjectMeta: metav1.ObjectMeta{Name: "sample-external"},
		Spec: infraApi.DatabaseProviderSpec{
			Type: infraApi.ProviderTypeExternal,
			External: &infraApi.ExternalProviderSpec{
				ConnectionSecretRef: corev1.SecretReference{
					Namespace: "db-admin",
					Name:      "external-admin",
				},
			},
		},
		Status: infraApi.DatabaseProviderStatus{
			Status: fwapi.Status{
				Conditions: []fwapi.Condition{{
					Type:   "Reachable",
					Status: metav1.ConditionTrue,
				}},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "external-admin", Namespace: "db-admin"},
		Data: map[string][]byte{
			postgres.SecretKeyHost:     []byte("postgres.db-admin.svc"),
			postgres.SecretKeyPort:     []byte("5432"),
			postgres.SecretKeyUser:     []byte("postgres"),
			postgres.SecretKeyPassword: []byte("secret"),
			postgres.SecretKeyDatabase: []byte("postgres"),
		},
	}
	claim := &infraApi.DatabaseClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "analytics", Namespace: "app"},
		Spec: infraApi.DatabaseClaimSpec{
			Provider:   infraApi.ProviderRef{Name: "sample-external"},
			Database:   "analytics",
			Extensions: []string{"vector"},
		},
		Status: infraApi.DatabaseClaimStatus{Provider: "sample-external"},
	}
	pg := &extensionProvisionPG{
		databases:          map[string]bool{"analytics": true},
		extensionAvailable: map[string]bool{"vector": false},
	}
	cli := fake.NewClientBuilder().WithScheme(claimTestScheme(t)).WithObjects(provider, secret, claim).Build()
	m := &Controller{Options: Options{
		cfg: &moduleconfig.Config{OperatorNamespace: "odh-db-operator-system"},
		PostgresClientFactory: func(_ context.Context, _ postgres.Config) (postgres.Client, error) {
			return pg, nil
		},
	}}

	err := m.provisionAction(context.Background(), claimTestRequest(t, cli, claim))
	g.Expect(err).To(HaveOccurred())
	var stop odherrors.StopError
	g.Expect(errors.As(err, &stop)).To(BeTrue())
	var missingExt claimerrors.ExtensionNotAvailable
	g.Expect(errors.As(err, &missingExt)).To(BeTrue())

	provisioned := conditions.FindStatusCondition(claim, ConditionProvisioned)
	g.Expect(provisioned).NotTo(BeNil())
	g.Expect(provisioned.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(provisioned.Reason).To(Equal("ExtensionNotAvailable"))
	g.Expect(provisioned.Message).To(ContainSubstring("vector"))
	g.Expect(pg.execSQLsContainingCreateExtension()).To(BeFalse())
}

func TestSchemaClaimAPIUnavailable(t *testing.T) {
	g := NewWithT(t)
	schemaclaimGVK := schema.GroupVersionKind{
		Group:   infraApi.GroupName,
		Version: "v1alpha1",
		Kind:    "SchemaClaim",
	}

	g.Expect(schemaClaimAPIUnavailable(&meta.NoKindMatchError{GroupKind: schemaclaimGVK.GroupKind()})).
		To(BeTrue(), "NoKindMatchError means the SchemaClaim CRD is absent")
	g.Expect(schemaClaimAPIUnavailable(apierrors.NewNotFound(
		schema.GroupResource{Group: infraApi.GroupName, Resource: "schemaclaims"},
		"",
	))).To(BeTrue(), "NotFound is returned by some discovery paths when the API is missing")
	g.Expect(schemaClaimAPIUnavailable(fmt.Errorf(
		"failed to get API group resources: unable to retrieve the complete list of server APIs: %w",
		apierrors.NewNotFound(schema.GroupResource{Group: infraApi.GroupName, Resource: "schemaclaims"}, ""),
	))).To(BeTrue(), "controller-runtime discovery failure must not stick cleanup")
	g.Expect(schemaClaimAPIUnavailable(errors.New("Unauthorized"))).To(BeFalse())
	g.Expect(schemaClaimAPIUnavailable(apierrors.NewForbidden(
		schema.GroupResource{Group: infraApi.GroupName, Resource: "schemaclaims"},
		"",
		errors.New("no RBAC"),
	))).To(BeFalse(), "Forbidden is a real misconfiguration and should retry")
}

type extensionProvisionPG struct {
	databases          map[string]bool
	extensionAvailable map[string]bool
	execSQLs           []string
}

func (p *extensionProvisionPG) Config() postgres.Config { return postgres.Config{} }
func (p *extensionProvisionPG) Close()                  {}
func (p *extensionProvisionPG) Ping(context.Context) error {
	return nil
}

func (p *extensionProvisionPG) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	p.execSQLs = append(p.execSQLs, sql)
	return pgconn.CommandTag{}, nil
}

func (p *extensionProvisionPG) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, fmt.Errorf("unexpected Query")
}

func (p *extensionProvisionPG) QueryRow(_ context.Context, query string, args ...any) (pgx.Row, error) {
	if p.databases == nil {
		p.databases = map[string]bool{}
	}
	name, _ := args[0].(string)
	switch {
	case strings.Contains(query, "pg_database"):
		return extensionBoolRow{p.databases[name]}, nil
	case strings.Contains(query, "pg_available_extensions"):
		avail := false
		if p.extensionAvailable != nil {
			avail = p.extensionAvailable[name]
		}
		return extensionBoolRow{avail}, nil
	default:
		return extensionBoolRow{false}, nil
	}
}

func (p *extensionProvisionPG) execSQLsContainingCreateExtension() bool {
	for _, sql := range p.execSQLs {
		if strings.Contains(strings.ToUpper(sql), "CREATE EXTENSION") {
			return true
		}
	}
	return false
}

type extensionBoolRow struct{ v bool }

func (r extensionBoolRow) Scan(dest ...any) error {
	ptr, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("expected *bool")
	}
	*ptr = r.v
	return nil
}

func claimTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	g := NewWithT(t)
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(infraApi.AddToScheme(scheme)).To(Succeed())
	return scheme
}

func claimTestRequest(t *testing.T, cli client.Client, claim *infraApi.DatabaseClaim) *odhtypes.ReconciliationRequest {
	t.Helper()
	aggregator, err := conditions.NewAggregator(
		fwapi.ConditionTypeReady,
		conditions.Dependent(fwapi.ConditionType(ConditionProvisioned), conditions.HealthyWhenTrue),
		conditions.Dependent(fwapi.ConditionType(ConditionTLSConfiguration), conditions.HealthyWhenTrue),
	)
	if err != nil {
		t.Fatal(err)
	}
	return &odhtypes.ReconciliationRequest{
		Client:     cli,
		Conditions: conditions.NewManager(claim, aggregator),
		Instance:   claim,
	}
}
