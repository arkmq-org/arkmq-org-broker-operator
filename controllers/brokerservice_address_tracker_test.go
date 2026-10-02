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

var _ = Describe("address tracker", func() {

	It("detects ownership from local address", Label(unitLabel), func() {
		tracker := newAddressTracker()

		localAddr := &broker.AddressRef{
			Address: "orders",
		}

		refAddr := &broker.AddressRef{
			Address:      "orders",
			AppNamespace: "other-ns",
			AppName:      "other-app",
		}

		entry1 := tracker.track(localAddr)
		entry2 := tracker.track(refAddr)

		Expect(tracker.names).To(HaveLen(1))
		Expect(tracker.names["orders"].isOwned).To(BeTrue())

		_ = entry1
		_ = entry2
	})

	It("marks reference-only address as not owned", Label(unitLabel), func() {
		tracker := newAddressTracker()

		refAddr := &broker.AddressRef{
			Address:      "shared-queue",
			AppNamespace: "owner-ns",
			AppName:      "owner-app",
		}

		tracker.track(refAddr)

		Expect(tracker.names["shared-queue"].isOwned).To(BeFalse())
	})

	It("local address takes precedence over reference", Label(unitLabel), func() {
		tracker := newAddressTracker()

		refAddr := &broker.AddressRef{
			Address:      "events",
			AppNamespace: "other-ns",
			AppName:      "other-app",
		}
		tracker.track(refAddr)

		localAddr := &broker.AddressRef{
			Address: "events",
		}
		tracker.track(localAddr)

		Expect(tracker.names["events"].isOwned).To(BeTrue())
	})
})
