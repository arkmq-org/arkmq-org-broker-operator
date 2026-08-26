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
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	specNamespaceDefaultBase    = "test"
	specNamespaceOtherBase      = "other"
	specNamespaceRestrictedBase = "restricted"
	specNamespace1Base          = "ns-one"
	specNamespace2Base          = "ns-two"
	specNamespace3Base          = "ns-three"

	specNamespaceHexBytes       = 3
	specNamespaceIsolationLabel = "test.arkmq.org/isolation"
	specNamespaceBaseLabel      = "test.arkmq.org/base"
	specNamespaceAllocAttempts  = 8
)

func namespaceIsolationDisabled() bool {
	if os.Getenv("DEPLOY_OPERATOR") == "true" {
		return true
	}
	if v, ok := os.LookupEnv("TEST_DISABLE_NS_PREFIX"); ok {
		disabled, err := strconv.ParseBool(v)
		return err == nil && disabled
	}
	return false
}

func shortHexID() string {
	b := make([]byte, specNamespaceHexBytes)
	if _, err := crand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to read random bytes for spec namespace: %v", err))
	}
	return hex.EncodeToString(b)
}

func uniqueSpecNamespace(base string) string {
	if namespaceIsolationDisabled() {
		return base
	}
	return base + "-" + shortHexID()
}

func specNamespaceBase(name string) string {
	const suffixLen = specNamespaceHexBytes * 2
	if len(name) <= suffixLen+1 {
		return name
	}
	dashAt := len(name) - suffixLen - 1
	if name[dashAt] != '-' {
		return name
	}
	suffix := name[dashAt+1:]
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		hexDigit := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !hexDigit {
			return name
		}
	}
	return name[:dashAt]
}

func assignSpecNamespaces() {
	defaultNamespace = uniqueSpecNamespace(specNamespaceDefaultBase)
	otherNamespace = uniqueSpecNamespace(specNamespaceOtherBase)
	restrictedNamespace = uniqueSpecNamespace(specNamespaceRestrictedBase)
	namespace1 = uniqueSpecNamespace(specNamespace1Base)
	namespace2 = uniqueSpecNamespace(specNamespace2Base)
	namespace3 = uniqueSpecNamespace(specNamespace3Base)
}

func retargetOperatorNamespace() {
	os.Setenv("OPERATOR_NAMESPACE", defaultNamespace)
	common.SetOperatorNameSpace(defaultNamespace)
}

func allocateDefaultSpecNamespace() {
	if namespaceIsolationDisabled() {
		return
	}

	var err error
	for i := 0; i < specNamespaceAllocAttempts; i++ {
		if i > 0 {
			defaultNamespace = uniqueSpecNamespace(specNamespaceDefaultBase)
		}
		err = createNamespace(defaultNamespace, nil)
		if err == nil {
			waitForNamespaceActive(defaultNamespace)
			waitForOpenShiftUidRange(defaultNamespace)
			return
		}
		if errors.IsAlreadyExists(err) {
			continue
		}
		Expect(err).NotTo(HaveOccurred())
		return
	}
	Expect(err).NotTo(HaveOccurred(), "failed to allocate a unique spec namespace")
}

func waitForNamespaceActive(namespace string) {
	Eventually(func(g Gomega) {
		ns := corev1.Namespace{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &ns)).Should(Succeed())
		g.Expect(ns.Status.Phase).To(Equal(corev1.NamespaceActive))
	}, "30s", "1s").Should(Succeed(), "namespace %s did not become Active", namespace)
}

func waitForOpenShiftUidRange(namespace string) {
	if !isOpenshift {
		return
	}
	Eventually(func(g Gomega) {
		ns := corev1.Namespace{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &ns)).Should(Succeed())
		uidRange := ns.Annotations["openshift.io/sa.scc.uid-range"]
		g.Expect(uidRange).ShouldNot(BeEmpty())
		uidRangeTokens := strings.Split(uidRange, "/")
		var parseErr error
		defaultUid, parseErr = strconv.ParseInt(uidRangeTokens[0], 10, 64)
		g.Expect(parseErr).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

func specNamespaceLabels(namespace string, securityPolicy *string) map[string]string {
	labels := map[string]string{}
	if !namespaceIsolationDisabled() {
		labels[specNamespaceIsolationLabel] = "true"
		labels[specNamespaceBaseLabel] = specNamespaceBase(namespace)
	}
	if securityPolicy != nil {
		labels["pod-security.kubernetes.io/audit"] = *securityPolicy
		labels["pod-security.kubernetes.io/enforce"] = *securityPolicy
		labels["pod-security.kubernetes.io/warn"] = *securityPolicy
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

func createNamespace(namespace string, securityPolicy *string) error {
	ns := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: specNamespaceLabels(namespace, securityPolicy),
		},
	}

	err := k8sClient.Create(ctx, &ns, &client.CreateOptions{})
	created := err == nil

	// Shared namespaces (prefix disabled) and envtest leftovers are expected to already exist.
	// envTest won't delete, get stuck in Terminating state:
	// https://github.com/kubernetes-sigs/controller-runtime/issues/880
	if errors.IsAlreadyExists(err) {
		if namespaceIsolationDisabled() || os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			err = nil
		}
	}

	if created && !namespaceIsolationDisabled() {
		DeferCleanup(func() {
			deleteNamespace(namespace, true, Default)
		})
	}
	return err
}

func deleteNamespace(namespace string, wait bool, g Gomega) {

	// envTest won't delete, get stuck in Terminating state
	// https://github.com/kubernetes-sigs/controller-runtime/issues/880
	if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
		return
	}
	ns := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	By("Deleting namespace: " + namespace)
	key := types.NamespacedName{Name: namespace}

	err := k8sClient.Get(ctx, key, &ns)
	if errors.IsNotFound(err) {
		return
	}
	g.Expect(err).Should(Succeed())

	zeroGracePeriodSeconds := int64(0) // immediate delete
	err = k8sClient.Delete(ctx, &ns, &client.DeleteOptions{GracePeriodSeconds: &zeroGracePeriodSeconds})
	g.Expect(err == nil || errors.IsNotFound(err)).To(BeTrue())

	if !wait {
		return
	}

	By("verifying gone: " + namespace)
	g.Eventually(func(g Gomega) {
		getErr := k8sClient.Get(ctx, key, &ns)
		if getErr == nil && verbose {
			fmt.Printf("\nNamespace %s Status: %v\n", namespace, ns.Status)
			fmt.Printf("\nNamespace %s Spec: %v\n", namespace, ns)

		}
		g.Expect(errors.IsNotFound(getErr)).To(BeTrue())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}
