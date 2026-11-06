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

package resources

import (
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("SetOwnerAndControllerWithLog", Label("unit"), func() {

	It("does not panic with nil owner and leaves owner references empty", Label("unit"), func() {
		target := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		}

		Expect(func() {
			SetOwnerAndControllerWithLog(logr.New(log.NullLogSink{}), nil, target)
		}).NotTo(Panic())

		Expect(target.GetOwnerReferences()).To(BeEmpty(),
			"nil owner should not set any owner references")
	})

	It("sets owner reference when owner is non-nil", Label("unit"), func() {
		owner := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-owner",
				Namespace: "default",
				UID:       types.UID("owner-uid-123"),
			},
		}

		target := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		}

		SetOwnerAndControllerWithLog(logr.New(log.NullLogSink{}), owner, target)

		refs := target.GetOwnerReferences()
		Expect(refs).To(HaveLen(1))
		Expect(refs[0].Name).To(Equal("my-owner"))
		Expect(refs[0].UID).To(Equal(types.UID("owner-uid-123")))
		Expect(*refs[0].Controller).To(BeTrue())
	})
})
