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

package appselector

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newFakeClientForCEL creates a fake client with namespace objects for CEL evaluation tests.
func newFakeClientForCEL(namespaces ...string) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1beta2.AddToScheme(scheme)

	objects := make([]runtime.Object, 0, len(namespaces))
	for _, ns := range namespaces {
		objects = append(objects, &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: ns,
			},
		})
	}

	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
}

var _ = Describe("CEL Program Caching", func() {
	var (
		expr       string
		app        *v1beta2.BrokerApp
		service    *v1beta2.BrokerService
		fakeClient client.Client
	)

	BeforeEach(func() {
		expr = `app.metadata.namespace.startsWith("team-")`

		app = &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-app",
				Namespace: "team-a",
			},
		}

		service = &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-service",
				Namespace: "broker-services",
			},
		}

		fakeClient = newFakeClientForCEL("team-a", "broker-services")

		// Clear the cache for clean test state
		celProgramCache.Purge()
	})

	It("caches and reuses CEL programs", func() {
		result1, err := evaluateExpression(expr, app, service, fakeClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result1).To(BeTrue())

		// Check that program is cached
		found := celProgramCache.Contains(expr)
		Expect(found).To(BeTrue(), "Program should be cached after first evaluation")

		result2, err := evaluateExpression(expr, app, service, fakeClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(result2).To(BeTrue())

		Expect(result1).To(Equal(result2))
	})
})

var _ = Describe("Concurrent Cache Access", func() {
	It("handles concurrent cache operations safely", func() {
		expr := `app.metadata.namespace.startsWith("concurrent-")`

		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-app",
				Namespace: "concurrent-test",
			},
		}

		service := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test",
			},
		}

		fakeClient := newFakeClientForCEL("concurrent-test", "test")

		// Clear the cache for clean test state
		celProgramCache.Purge()

		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				defer GinkgoRecover()
				for j := 0; j < 100; j++ {
					_, err := evaluateExpression(expr, app, service, fakeClient)
					Expect(err).NotTo(HaveOccurred())
				}
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		// Check that program is cached after concurrent access
		found := celProgramCache.Contains(expr)
		Expect(found).To(BeTrue(), "Program should be cached after concurrent access")
	})
})
