/*
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

package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const isolationPoisonSecretName = "broker-user-cred-credentials-secret"

var isolationFirstSpecNamespace string

var _ = Describe("spec namespace isolation", Label("ns-isolation"), func() {

	Context("poison secret across specs", Ordered, func() {

		It("plants a well-known secret in this spec's namespace", func() {
			if namespaceIsolationDisabled() {
				Skip("namespace prefixing disabled")
			}

			Expect(defaultNamespace).To(MatchRegexp(`^test-[0-9a-f]{6}$`))
			isolationFirstSpecNamespace = defaultNamespace

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      isolationPoisonSecretName,
					Namespace: defaultNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		})

		It("creates the same secret name in a different spec namespace", func() {
			if namespaceIsolationDisabled() {
				Skip("namespace prefixing disabled")
			}

			Expect(defaultNamespace).To(MatchRegexp(`^test-[0-9a-f]{6}$`))
			Expect(defaultNamespace).NotTo(Equal(isolationFirstSpecNamespace))

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      isolationPoisonSecretName,
					Namespace: defaultNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		})
	})
})
