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

package e2e

import (
	"testing"

	. "github.com/onsi/gomega"
	pgclaim "github.com/opendatahub-io/odh-platform-utilities/framework/postgres"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClaimLibpqConnection(t *testing.T) {
	t.Parallel()

	t.Run("disable-without-ca", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					pgclaim.AnnotationConnectionTypeProtocol: pgclaim.ConnectionTypeProtocolPostgres,
				},
			},
			Data: map[string][]byte{
				pgclaim.SecretKeyHost:     []byte("postgres.example.svc"),
				pgclaim.SecretKeyPort:     []byte("5432"),
				pgclaim.SecretKeyUser:     []byte("claim_user"),
				pgclaim.SecretKeyPassword: []byte("s3cret"),
				pgclaim.SecretKeyDatabase: []byte("appdb"),
				pgclaim.SecretKeySSLMode:  []byte(pgclaim.SSLModeDisable),
			},
		}
		conn, params, err := claimLibpqConnection(secret, "")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(params.TLSEnabled()).To(BeFalse())
		g.Expect(expectedSSLFlag(params)).To(Equal("f"))
		g.Expect(conn).To(ContainSubstring("host='postgres.example.svc'"))
		g.Expect(conn).To(ContainSubstring("dbname='appdb'"))
		g.Expect(conn).To(ContainSubstring("sslmode='disable'"))
		g.Expect(conn).NotTo(ContainSubstring("sslrootcert"))
	})

	t.Run("verify-full-with-ca-path", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					pgclaim.AnnotationConnectionTypeProtocol: pgclaim.ConnectionTypeProtocolPostgres,
				},
			},
			Data: map[string][]byte{
				pgclaim.SecretKeyHost:     []byte("rhai-db.database-system.svc"),
				pgclaim.SecretKeyPort:     []byte("5432"),
				pgclaim.SecretKeyUser:     []byte("claim_user"),
				pgclaim.SecretKeyPassword: []byte("s3cret"),
				pgclaim.SecretKeyDatabase: []byte("appdb"),
				pgclaim.SecretKeySSLMode:  []byte(pgclaim.SSLModeVerifyFull),
				pgclaim.SecretKeyCA:       []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
			},
		}
		conn, params, err := claimLibpqConnection(secret, consumerCAFile)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(params.TLSEnabled()).To(BeTrue())
		g.Expect(expectedSSLFlag(params)).To(Equal("t"))
		g.Expect(conn).To(ContainSubstring("sslmode='verify-full'"))
		g.Expect(conn).To(ContainSubstring("sslrootcert='" + consumerCAFile + "'"))
		g.Expect(conn).To(ContainSubstring("gssencmode='disable'"))
	})

	t.Run("schema-sets-search-path", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		secret := &corev1.Secret{
			Data: map[string][]byte{
				pgclaim.SecretKeyHost:     []byte("postgres.example.svc"),
				pgclaim.SecretKeyUser:     []byte("claim_user"),
				pgclaim.SecretKeyPassword: []byte("s3cret"),
				pgclaim.SecretKeyDatabase: []byte("appdb"),
				pgclaim.SecretKeySSLMode:  []byte(pgclaim.SSLModeDisable),
				pgclaim.SecretKeySchema:   []byte("claim_schema_abc"),
			},
		}
		conn, params, err := claimLibpqConnection(secret, "")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(params.Schema).To(Equal("claim_schema_abc"))
		g.Expect(conn).To(ContainSubstring("options='-csearch_path=claim_schema_abc'"))
	})

	t.Run("verify-full-rejects-empty-ca-file", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		secret := &corev1.Secret{
			Data: map[string][]byte{
				pgclaim.SecretKeyHost:     []byte("postgres.example.svc"),
				pgclaim.SecretKeyUser:     []byte("claim_user"),
				pgclaim.SecretKeyPassword: []byte("s3cret"),
				pgclaim.SecretKeyDatabase: []byte("appdb"),
				pgclaim.SecretKeySSLMode:  []byte(pgclaim.SSLModeVerifyFull),
				pgclaim.SecretKeyCA:       []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
			},
		}
		_, _, err := claimLibpqConnection(secret, "")
		g.Expect(err).To(HaveOccurred())
	})
}

func TestAccessorConsumerPod(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	secret := &corev1.Secret{
		Data: map[string][]byte{
			pgclaim.SecretKeyHost:     []byte("postgres.example.svc"),
			pgclaim.SecretKeyUser:     []byte("claim_user"),
			pgclaim.SecretKeyPassword: []byte("s3cret"),
			pgclaim.SecretKeyDatabase: []byte("appdb"),
			pgclaim.SecretKeySSLMode:  []byte(pgclaim.SSLModeVerifyFull),
			pgclaim.SecretKeyCA:       []byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
		},
	}
	connection, params, err := claimLibpqConnection(secret, consumerCAFile)
	g.Expect(err).NotTo(HaveOccurred())

	pod := accessorConsumerPod("claim-ns", "claim-secret", "postgres:16", connection, expectedSSLFlag(params), true)
	g.Expect(pod.Name).To(Equal("accessor-connection-consumer"))
	g.Expect(pod.Namespace).To(Equal("claim-ns"))
	g.Expect(pod.Spec.Containers).To(HaveLen(1))
	g.Expect(pod.Spec.Containers[0].Command).To(Equal([]string{"/bin/sh", "-ec", accessorConsumerScript}))
	g.Expect(pod.Spec.Containers[0].Env).To(ContainElements(
		corev1.EnvVar{Name: "CONNECTION", Value: connection},
		corev1.EnvVar{Name: "EXPECTED_SSL", Value: "t"},
	))
	g.Expect(pod.Spec.Volumes).To(HaveLen(1))
	g.Expect(pod.Spec.Volumes[0].Secret).NotTo(BeNil())
	g.Expect(pod.Spec.Volumes[0].Secret.SecretName).To(Equal("claim-secret"))
	g.Expect(pod.Spec.Volumes[0].Secret.Items).To(Equal([]corev1.KeyToPath{{
		Key:  pgclaim.SecretKeyCA,
		Path: "ca.crt",
	}}))
	g.Expect(pod.Spec.Containers[0].VolumeMounts).To(Equal([]corev1.VolumeMount{{
		Name: "trust", MountPath: "/etc/db-trust", ReadOnly: true,
	}}))
}

func TestAccessorSearchPathPod(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	secret := &corev1.Secret{
		Data: map[string][]byte{
			pgclaim.SecretKeyHost:     []byte("postgres.example.svc"),
			pgclaim.SecretKeyUser:     []byte("claim_user"),
			pgclaim.SecretKeyPassword: []byte("s3cret"),
			pgclaim.SecretKeyDatabase: []byte("appdb"),
			pgclaim.SecretKeySSLMode:  []byte(pgclaim.SSLModeDisable),
			pgclaim.SecretKeySchema:   []byte("claim_schema_xyz"),
		},
	}
	connection, params, err := claimLibpqConnection(secret, "")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(params.Schema).To(Equal("claim_schema_xyz"))
	g.Expect(connection).To(ContainSubstring("options='-csearch_path=claim_schema_xyz'"))

	pod := accessorSearchPathPod("claim-ns", "schema-secret", "postgres:16", connection, params.Schema, false)
	g.Expect(pod.Name).To(Equal("accessor-search-path-consumer"))
	g.Expect(pod.Spec.Containers[0].Command).To(Equal([]string{"/bin/sh", "-ec", accessorSearchPathScript}))
	g.Expect(pod.Spec.Containers[0].Env).To(ContainElements(
		corev1.EnvVar{Name: "CONNECTION", Value: connection},
		corev1.EnvVar{Name: "EXPECTED_SCHEMA", Value: "claim_schema_xyz"},
	))
	g.Expect(pod.Spec.Volumes).To(BeEmpty())
}
