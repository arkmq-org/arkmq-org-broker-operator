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
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("port assignment", func() {

	Context("assignNextAvailablePort", func() {
		It("assigns first port when none used", Label(unitLabel), func() {
			usedPorts := make(map[int32]bool)

			port, err := assignNextAvailablePort(usedPorts)

			Expect(err).NotTo(HaveOccurred())
			Expect(port).To(Equal(int32(61616)))
		})

		It("assigns next port after used ones", Label(unitLabel), func() {
			usedPorts := map[int32]bool{
				61616: true,
				61617: true,
			}

			port, err := assignNextAvailablePort(usedPorts)

			Expect(err).NotTo(HaveOccurred())
			Expect(port).To(Equal(int32(61618)))
		})

		It("assigns last available port", Label(unitLabel), func() {
			usedPorts := make(map[int32]bool)
			for port := int32(61616); port < 65535; port++ {
				usedPorts[port] = true
			}

			port, err := assignNextAvailablePort(usedPorts)

			Expect(err).NotTo(HaveOccurred())
			Expect(port).To(Equal(int32(65535)))
		})

		It("returns error when all ports exhausted", Label(unitLabel), func() {
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

	Context("collectUsedPorts", func() {
		It("returns empty map for no apps", Label(unitLabel), func() {
			apps := []v1beta2.BrokerApp{}

			used := collectUsedPorts(apps, nil)

			Expect(used).To(BeEmpty())
		})

		It("collects port from single app", Label(unitLabel), func() {
			apps := []v1beta2.BrokerApp{
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

		It("collects ports from multiple apps", Label(unitLabel), func() {
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

			used := collectUsedPorts(apps, nil)

			Expect(used).To(HaveLen(2))
			Expect(used[61616]).To(BeTrue())
			Expect(used[61617]).To(BeTrue())
		})

		It("excludes specified app", Label(unitLabel), func() {
			excludeApp := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-to-exclude",
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

		It("skips apps without service binding", Label(unitLabel), func() {
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

		It("skips apps with zero port", Label(unitLabel), func() {
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
