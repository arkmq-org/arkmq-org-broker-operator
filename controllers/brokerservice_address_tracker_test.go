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
	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AddressTracker with Ownership Detection", func() {
	Context("When tracking addresses", func() {
		It("should correctly detect ownership", func() {
			tracker := newAddressTracker()
			// Local address (owned)
			localAddr := &broker.AddressRef{
				Address: "orders",
				// AppNamespace and AppName empty = owned
			}
			entry := tracker.track(localAddr)
			Expect(entry.isOwned).To(BeTrue())

			// Cross-app reference (not owned)
			refAddr := &broker.AddressRef{
				Address:      "orders",
				AppNamespace: "other-ns",
				AppName:      "other-app",
			}
			entry = tracker.track(refAddr)
			Expect(entry.isOwned).To(BeTrue())

			// Verify ownership
			Expect(tracker.names["orders"].isOwned).To(BeTrue())
		})
	})
})

var _ = Describe("AddressTracker with Reference Tracking", func() {
	Context("When tracking references", func() {
		It("should not mark an address as owned when only cross-app references are tracked", func() {
			tracker := newAddressTracker()

			// Cross-app reference (not owned)
			refAddr := &broker.AddressRef{
				Address:      "shared-queue",
				AppNamespace: "other-ns",
				AppName:      "other-app",
			}
			entry := tracker.track(refAddr)
			Expect(entry.isOwned).To(BeFalse())

			// Verify ownership
			Expect(tracker.names["shared-queue"].isOwned).To(BeFalse())
		})
	})
})

var _ = Describe("AddressTracker with Local Address Precedence", func() {
	Context("When tracking addresses in different orders", func() {
		It("should mark as owned when local address is  tracked after reference", func() {
			tracker := newAddressTracker()

			// Track reference first
			refAddr := &broker.AddressRef{
				Address:      "events",
				AppNamespace: "other-ns",
				AppName:      "other-app",
			}
			tracker.track(refAddr)

			// Then track local ownership
			localAddr := &broker.AddressRef{
				Address: "events",
				// Empty AppNamespace/AppName = owned
			}
			tracker.track(localAddr)

			// Verify that the entry is owned
			Expect(tracker.names["events"].isOwned).To(BeTrue())
		})
	})
})
