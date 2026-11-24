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
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	pgclaim "github.com/opendatahub-io/odh-platform-utilities/framework/postgres"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// consumerCAFile is the in-pod path where claim Secret ca.crt is mounted.
// Libpq must use this path; WriteCACert would write on the test host.
const consumerCAFile = "/etc/db-trust/ca.crt"

// accessorConnectionSecretKey holds the Libpq string in a short-lived Secret
// so the password is not persisted on the Pod spec via EnvVar.Value.
const accessorConnectionSecretKey = "connection"

// accessorConsumerScript runs the claim-contract insert/select against a
// connection string composed by framework/postgres. It does not assemble
// host/user/password fields in the shell.
const accessorConsumerScript = `
set -eu
: "${CONNECTION:?CONNECTION must be set from the claim-Secret accessor}"
: "${EXPECTED_SSL:?EXPECTED_SSL must be t or f}"
expected="${EXPECTED_SSL}|42"
query='INSERT INTO public.claim_contract_test VALUES (42);'
query="$query SELECT ssl, (SELECT value FROM public.claim_contract_test WHERE value = 42 LIMIT 1)"
query="$query FROM pg_stat_ssl WHERE pid = pg_backend_pid()"
result=$(psql -X -qAt -v ON_ERROR_STOP=1 "$CONNECTION" -c "$query")
test "$result" = "$expected"
`

// claimLibpqConnection parses a claim Secret with framework/postgres and
// returns a libpq connection string. caFile must be the path available inside
// the consumer pod when sslmode needs sslrootcert; pass "" when no CA file is
// required. Empty sslmode defaults to verify-full when caFile is set and
// disable otherwise, matching Internal/External claim Secret conventions.
func claimLibpqConnection(secret *corev1.Secret, caFile string) (string, pgclaim.Params, error) {
	params, err := pgclaim.ParseClaimSecret(secret)
	if err != nil {
		return "", params, fmt.Errorf("parse claim Secret: %w", err)
	}
	if params.SSLMode == "" {
		if caFile != "" {
			params.SSLMode = pgclaim.SSLModeVerifyFull
		} else {
			params.SSLMode = pgclaim.SSLModeDisable
		}
	}
	conn, err := params.Libpq(caFile)
	if err != nil {
		return "", params, fmt.Errorf("compose libpq connection string: %w", err)
	}
	return conn, params, nil
}

func expectedSSLFlag(tlsExpected bool) string {
	if tlsExpected {
		return "t"
	}
	return "f"
}

func caFileForConsumer(withCA bool) string {
	if withCA {
		return consumerCAFile
	}
	return ""
}

// accessorSearchPathScript proves Libpq options=-csearch_path=… from a
// SchemaClaim Secret: unqualified DDL lands in the claimed schema, not public.
const accessorSearchPathScript = `
set -eu
: "${CONNECTION:?CONNECTION must be set from the claim-Secret accessor}"
: "${EXPECTED_SCHEMA:?EXPECTED_SCHEMA must match the claim Secret schema}"
result=$(psql -X -qAt -v ON_ERROR_STOP=1 "$CONNECTION" <<'SQL'
CREATE TABLE IF NOT EXISTS search_path_probe (value integer);
TRUNCATE search_path_probe;
INSERT INTO search_path_probe VALUES (99);
SELECT current_schema() || '|' || (
  SELECT n.nspname
  FROM pg_class c
  JOIN pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relname = 'search_path_probe' AND c.relkind = 'r' AND n.nspname = current_schema()
) || '|' || (
  SELECT CASE WHEN EXISTS (
    SELECT 1
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE c.relname = 'search_path_probe' AND c.relkind = 'r' AND n.nspname = 'public'
  ) THEN 'in_public' ELSE 'not_in_public' END
);
SQL
)
expected="${EXPECTED_SCHEMA}|${EXPECTED_SCHEMA}|not_in_public"
test "$result" = "$expected"
`

// runAccessorConsumer proves the claim Secret using a libpq string from
// framework/postgres, then the same contract SQL as runConsumer.
func runAccessorConsumer(
	t *testing.T,
	ctx context.Context,
	cli client.Client,
	restCfg *rest.Config,
	namespace, secretName, image string,
	withCA bool,
) {
	t.Helper()
	g := NewWithT(t)

	secret := &corev1.Secret{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)).To(Succeed())

	connection, _, err := claimLibpqConnection(secret, caFileForConsumer(withCA))
	g.Expect(err).NotTo(HaveOccurred())

	connSecret := createAccessorConnectionSecret(t, ctx, cli, secret, connection)
	pod := accessorConsumerPod(namespace, secretName, connSecret, image, expectedSSLFlag(withCA), withCA)
	runPod(t, ctx, cli, restCfg, pod)
}

// runAccessorSearchPathConsumer proves a SchemaClaim Secret's accessor Libpq
// string sets search_path so unqualified writes land in the claimed schema.
func runAccessorSearchPathConsumer(
	t *testing.T,
	ctx context.Context,
	cli client.Client,
	restCfg *rest.Config,
	namespace, secretName, image string,
	withCA bool,
) {
	t.Helper()
	g := NewWithT(t)

	secret := &corev1.Secret{}
	g.Expect(cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: secretName}, secret)).To(Succeed())

	connection, params, err := claimLibpqConnection(secret, caFileForConsumer(withCA))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(params.Schema).NotTo(BeEmpty(), "SchemaClaim Secret must carry schema for search_path proof")
	g.Expect(strings.Contains(connection, "options='-csearch_path="+params.Schema+"'")).
		To(BeTrue(), "accessor connection must set the claimed search_path")

	connSecret := createAccessorConnectionSecret(t, ctx, cli, secret, connection)
	runPod(t, ctx, cli, restCfg, accessorSearchPathPod(namespace, secretName, connSecret, image, params.Schema, withCA))
}

func createAccessorConnectionSecret(
	t *testing.T,
	ctx context.Context,
	cli client.Client,
	claimSecret *corev1.Secret,
	connection string,
) string {
	t.Helper()
	g := NewWithT(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "accessor-conn-",
			Namespace:    claimSecret.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Secret",
				Name:       claimSecret.Name,
				UID:        claimSecret.UID,
			}},
		},
		Data: map[string][]byte{accessorConnectionSecretKey: []byte(connection)},
	}
	g.Expect(cli.Create(ctx, secret)).To(Succeed())
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		err := cli.Delete(cleanupCtx, secret, client.Preconditions{UID: &secret.UID})
		if err != nil && !apierrors.IsNotFound(err) && !apierrors.IsConflict(err) {
			t.Errorf("cleaning accessor connection Secret: %v", err)
		}
	})
	return secret.Name
}

func connectionFromSecret(secretName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: "CONNECTION",
		ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  accessorConnectionSecretKey,
		}},
	}
}

func accessorConsumerPod(
	namespace, claimSecretName, connSecretName, image, expectedSSL string,
	withCA bool,
) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "accessor-connection-consumer", Namespace: namespace},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "consumer",
				Image:   image,
				Command: []string{"/bin/sh", "-ec", accessorConsumerScript},
				Env: []corev1.EnvVar{
					connectionFromSecret(connSecretName),
					{Name: "EXPECTED_SSL", Value: expectedSSL},
				},
			}},
		},
	}
	mountClaimCA(pod, claimSecretName, withCA)
	return pod
}

func accessorSearchPathPod(
	namespace, claimSecretName, connSecretName, image, expectedSchema string,
	withCA bool,
) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "accessor-search-path-consumer", Namespace: namespace},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "consumer",
				Image:   image,
				Command: []string{"/bin/sh", "-ec", accessorSearchPathScript},
				Env: []corev1.EnvVar{
					connectionFromSecret(connSecretName),
					{Name: "EXPECTED_SCHEMA", Value: expectedSchema},
				},
			}},
		},
	}
	mountClaimCA(pod, claimSecretName, withCA)
	return pod
}

func mountClaimCA(pod *corev1.Pod, secretName string, withCA bool) {
	if !withCA {
		return
	}
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
		Name: "trust", MountPath: "/etc/db-trust", ReadOnly: true,
	}}
	pod.Spec.Volumes = []corev1.Volume{{Name: "trust", VolumeSource: corev1.VolumeSource{
		Secret: &corev1.SecretVolumeSource{
			SecretName: secretName,
			Items: []corev1.KeyToPath{{
				Key:  pgclaim.SecretKeyCA,
				Path: "ca.crt",
			}},
		},
	}}}
}
