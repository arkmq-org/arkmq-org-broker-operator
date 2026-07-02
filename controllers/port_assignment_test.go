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

package controllers

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Port Assignment", func() {
	Context("when assigning next available port", func() {
		It("should assign the first port when no ports are in use", func() {
			usedPorts := make(map[int32]bool)
			port, err := assignNextAvailablePort(usedPorts)
			Expect(err).ToNot(HaveOccurred())
			Expect(port).To(Equal(int32(61616)))
		})
		It("should assign the next available port when the first is already taken", func() {
			usedPorts := map[int32]bool{
				61616: true,
				61617: true,
			}
			port, err := assignNextAvailablePort(usedPorts)
			Expect(err).ToNot(HaveOccurred())
			Expect(port).To(Equal(int32(61618)))
		})
		It("should assign the last available port when all others are taken", func() {
			usedPorts := make(map[int32]bool)
			for port := int32(61616); port < 65535; port++ {
				usedPorts[port] = true
			}
			port, err := assignNextAvailablePort(usedPorts)
			Expect(err).ToNot(HaveOccurred())
			Expect(port).To(Equal(int32(65535)))
		})

		It("should return an error when all ports are exhausted", func() {
			usedPorts := make(map[int32]bool)
			for port := int32(61616); port <= 65535; port++ {
				usedPorts[port] = true
			}

			port, err := assignNextAvailablePort(usedPorts)

			Expect(err).To(HaveOccurred())
			Expect(port).To(Equal(int32(0)))
			Expect(err.Error()).To(ContainSubstring("exhausted"))
			Expect(err.Error()).To(ContainSubstring("61616"))
			Expect(err.Error()).To(ContainSubstring("65535"))
		})
	})

	Context("When collecting used ports", func() {
		It("should return empty map when no app exists", func() {
			apps := []v1beta2.BrokerApp{}
			usedPorts := collectUsedPorts(apps, nil)
			Expect(usedPorts).To(BeEmpty())
		})
		It("should collect port from single app", func() {
			apps := []v1beta2.BrokerApp{
				{
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61616,
						},
					},
				},
			}
			usedPorts := collectUsedPorts(apps, nil)
			Expect(usedPorts).To(HaveLen(1))
			Expect(usedPorts[61616]).To(BeTrue())
		})
		It("should collect port from multiple ports", func() {
			apps := []v1beta2.BrokerApp{
				{
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61616,
						},
					},
				},
				{
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61617,
						},
					},
				},
			}
			usedPorts := collectUsedPorts(apps, nil)

			Expect(usedPorts).To(HaveLen(2))
			Expect(usedPorts[61616]).To(BeTrue())
			Expect(usedPorts[61617]).To(BeTrue())
		})
		It("should exclude specific app from collection", func() {
			excludeApp := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "apps-to-exclude",
					Namespace: "test",
				},
				Status: v1beta2.BrokerAppStatus{
					Service: &v1beta2.BrokerServiceBindingStatus{
						AssignedPort: 61616,
					},
				},
			}

			apps := []v1beta2.BrokerApp{
				*excludeApp,
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "app-to-include",
						Namespace: "test",
					},
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61617,
						},
					},
				},
			}

			used := collectUsedPorts(apps, excludeApp)

			Expect(used).To(HaveLen(1))
			Expect(used[61616]).To(BeFalse())
			Expect(used[61617]).To(BeTrue())
		})
		It("should skip apps without service binding", func() {
			apps := []v1beta2.BrokerApp{
				{
					Status: v1beta2.BrokerAppStatus{
						Service: nil,
					},
				},
				{
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61616,
						},
					},
				},
			}

			used := collectUsedPorts(apps, nil)

			Expect(used).To(HaveLen(1))
			Expect(used[61616]).To(BeTrue())
		})

		It("should skip apps with zero port assignment", func() {
			apps := []v1beta2.BrokerApp{
				{
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 0,
						},
					},
				},
				{
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61616,
						},
					},
				},
			}

			used := collectUsedPorts(apps, nil)

			Expect(used).To(HaveLen(1))
			Expect(used[61616]).To(BeTrue())
			Expect(used[0]).To(BeFalse())
		})
	})
})
