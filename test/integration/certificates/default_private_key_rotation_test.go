/*
Copyright 2026 The cert-manager Authors.

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

package certificates

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"github.com/cert-manager/cert-manager/integration-tests/framework"
	"github.com/cert-manager/cert-manager/internal/controller/feature"
	cmapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	utilfeature "github.com/cert-manager/cert-manager/pkg/util/feature"
	"github.com/cert-manager/cert-manager/pkg/util/pki"
)

// Exercise the real API and Certificate controllers, comparing the public key
// in the generated CSR with a pre-existing target Secret. Do not parallelize:
// the feature gate is process-wide.
func TestDefaultPrivateKeyRotation(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("gate=%t", enabled), func(t *testing.T) {
			featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, feature.DefaultPrivateKeyRotationPolicyAlways, enabled)
			config, stop := framework.RunControlPlane(t)
			t.Cleanup(stop)
			stopControllers := runAllControllers(t, config)
			t.Cleanup(stopControllers)
			kube, _, cm, _, _ := framework.NewClients(t, config)
			for _, tc := range []struct {
				name   string
				key    *cmapi.CertificatePrivateKey
				rotate bool
			}{
				{"omitted", nil, enabled},
				{"empty", &cmapi.CertificatePrivateKey{}, enabled},
				{"always", &cmapi.CertificatePrivateKey{RotationPolicy: cmapi.RotationPolicyAlways}, true},
				{"never", &cmapi.CertificatePrivateKey{RotationPolicy: cmapi.RotationPolicyNever}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					crt := &cmapi.Certificate{
						ObjectMeta: metav1.ObjectMeta{Name: tc.name},
						Spec: cmapi.CertificateSpec{
							SecretName: tc.name, DNSNames: []string{"rotation.example.test"},
							IssuerRef: cmmeta.IssuerReference{Name: "issuer"}, PrivateKey: tc.key,
						},
					}
					oldKey, err := pki.GeneratePrivateKeyForCertificate(crt)
					require.NoError(t, err)
					encoded, err := pki.EncodePrivateKey(oldKey, cmapi.PKCS1)
					require.NoError(t, err)
					_, err = kube.CoreV1().Secrets("default").Create(t.Context(), &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: tc.name},
						Data:       map[string][]byte{corev1.TLSPrivateKeyKey: encoded},
					}, metav1.CreateOptions{})
					require.NoError(t, err)
					_, err = cm.CertmanagerV1().Certificates("default").Create(t.Context(), crt, metav1.CreateOptions{})
					require.NoError(t, err)
					var request []byte
					require.NoError(t, wait.PollUntilContextTimeout(t.Context(), 100*time.Millisecond, 30*time.Second, true, func(ctx context.Context) (bool, error) {
						requests, err := cm.CertmanagerV1().CertificateRequests("default").List(ctx, metav1.ListOptions{})
						if err != nil {
							return false, err
						}
						for _, req := range requests.Items {
							for _, owner := range req.OwnerReferences {
								if owner.Kind == "Certificate" && owner.Name == tc.name {
									request = req.Spec.Request
									return true, nil
								}
							}
						}
						return false, nil
					}))
					csr, err := pki.DecodeX509CertificateRequestBytes(request)
					require.NoError(t, err)
					equal, err := pki.PublicKeysEqual(oldKey.Public(), csr.PublicKey)
					require.NoError(t, err)
					require.Equal(t, !tc.rotate, equal, "CSR must use the expected private key")
					t.Logf("gate=%t policy=%s rotated=%t", enabled, tc.name, !equal)
				})
			}
		})
	}
}
