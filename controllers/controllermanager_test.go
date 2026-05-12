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
	"fmt"
	"os"
	"strconv"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/namer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("tests regarding controller manager", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("operator namespaces test", func() {

		It("test resolving watching namespace", func() {

			operatorNamespace := "default"
			isLocal, watchList := common.ResolveWatchNamespaceForManager(operatorNamespace, operatorNamespace)
			Expect(isLocal).To(BeTrue())
			Expect(watchList).To(BeNil())

			for _, wn := range []string{"", "*"} {
				isLocal, watchList = common.ResolveWatchNamespaceForManager(operatorNamespace, wn)
				Expect(isLocal).To(BeFalse())
				Expect(watchList).To(BeNil())
			}

			isLocal, watchList = common.ResolveWatchNamespaceForManager(operatorNamespace, "namespace1,namespace2")
			Expect(isLocal).To(BeFalse())
			Expect(len(watchList)).To(Equal(2))
			Expect(watchList[0]).To(Equal("namespace1"))
			Expect(watchList[1]).To(Equal("namespace2"))
		})

		It("test watching single default namespace", func() {
			testWatchNamespace("single", Default, func(g Gomega) {
				By("deploying broker in to target namespace")
				cr, createdCr := DeployCustomBroker(defaultNamespace, nil)

				By("check statefulset get created")
				createdSs := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(cr.Name), Namespace: defaultNamespace}
					err := k8sClient.Get(ctx, key, createdSs)
					g.Expect(err).To(Succeed(), "expect to get ss for cr "+cr.Name)
				}, timeout, interval).Should(Succeed())

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

					// with kube, deleting while initialising leads to long delays on terminating the namespace..

					By("verifying started")
					deployedCrd := brokerv1beta1.ActiveMQArtemis{}
					key := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}

					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, key, &deployedCrd)).Should(Succeed())
						g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("deploying broker in " + namespace1)
				cr1, createdCr1 := DeployCustomBroker(namespace1, nil)

				By("check statefulset should not be created")
				createdSs1 := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(cr1.Name), Namespace: namespace1}
					err := k8sClient.Get(ctx, key, createdSs1)
					g.Expect(err).NotTo(Succeed(), "no ss should be created for cr "+cr1.Name+" in namespace "+namespace1)
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(cr1.Name), Namespace: defaultNamespace}
					err := k8sClient.Get(ctx, key, createdSs1)
					g.Expect(err).NotTo(Succeed(), "no ss should be created for cr "+cr1.Name+" in namespace "+defaultNamespace)
				}, timeout, interval).Should(Succeed())

				CleanResource(createdCr, cr.Name, defaultNamespace)
				CleanResource(createdCr1, cr1.Name, namespace1)
			})
		})

		It("test watching restricted namespace", Label("verySlow"), func() {
			testWatchNamespace("restricted", Default, func(g Gomega) {
				By("deploying broker in to target namespace")
				cr, createdCr := DeployCustomBroker(restrictedNamespace, func(c *brokerv1beta1.ActiveMQArtemis) {
					c.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
					c.Spec.DeploymentPlan.PersistenceEnabled = true
					c.Spec.DeploymentPlan.MessageMigration = common.NewTrue()
				})

				By("check statefulset get created")
				createdSs := &appsv1.StatefulSet{}
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(cr.Name), Namespace: restrictedNamespace}
					err := k8sClient.Get(ctx, key, createdSs)
					g.Expect(err).To(Succeed(), "expect to get ss for cr "+cr.Name)
				}, timeout, interval).Should(Succeed())

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

					// with kube, deleting while initialising leads to long delays on terminating the namespace..

					By("verifying started")
					deployedCrd := brokerv1beta1.ActiveMQArtemis{}
					key := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}

					Eventually(func(g Gomega) {
						g.Expect(k8sClient.Get(ctx, key, &deployedCrd)).Should(Succeed())
						g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(2))
						g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
						g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

					By("sending message to pod for scale down")
					podWithOrdinal := namer.CrToSS(createdCr.Name) + "-1"
					Eventually(func(g Gomega) {

						sendCmd := []string{"amq-broker/bin/artemis", "producer", "--user", "Jay", "--password", "activemq", "--url", "tcp://" + podWithOrdinal + ":61616", "--message-count", "1", "--destination", "TEST", "--verbose"}
						content, err := RunCommandInPodWithNamespace(podWithOrdinal, restrictedNamespace, createdCr.Name+"-container", sendCmd)
						g.Expect(err).To(BeNil())
						g.Expect(*content).Should(ContainSubstring("Produced: 1 messages"))

					}, timeout, interval).Should(Succeed())

					By("Scaling down to pod 0")
					podWithOrdinal = namer.CrToSS(createdCr.Name) + "-0"
					Eventually(func(g Gomega) {
						getPersistedVersionedCrd(createdCr.Name, restrictedNamespace, createdCr)
						createdCr.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
						k8sClient.Update(ctx, createdCr)
						g.Expect(len(createdCr.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
						By("Checking messsage count on broker 0")
						curlUrl := "http://" + podWithOrdinal + ":8161/console/jolokia/read/org.apache.activemq.artemis:address=%22TEST%22,broker=%22amq-broker%22,component=addresses,queue=%22TEST%22,routing-type=%22anycast%22,subcomponent=queues/MessageCount"
						queryCmd := []string{"curl", "-k", "-H", "Origin: http://localhost:8161", "-u", "user:password", curlUrl}
						reply, err := RunCommandInPodWithNamespace(podWithOrdinal, restrictedNamespace, createdCr.Name+"-container", queryCmd)
						g.Expect(err).To(BeNil())
						g.Expect(*reply).To(ContainSubstring("\"value\":1"))

					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				CleanResource(createdCr, cr.Name, restrictedNamespace)
			})
		})

		It("test watching all namespaces", func() {
			testWatchNamespace("all", Default, func(g Gomega) {
				By("deploying broker in to all namespaces")
				var createdCrs []*brokerv1beta1.ActiveMQArtemis
				for _, ns := range []string{defaultNamespace, namespace1, namespace2, namespace3} {
					_, createdCr := DeployCustomBroker(ns, nil)
					createdCrs = append(createdCrs, createdCr)
				}

				By("check statefulset get created in each namespace")
				for _, createdCr := range createdCrs {
					createdSs := &appsv1.StatefulSet{}
					key := types.NamespacedName{Name: namer.CrToSS(createdCr.Name), Namespace: createdCr.Namespace}
					g.Eventually(func(g Gomega) {
						err := k8sClient.Get(ctx, key, createdSs)
						g.Expect(err).To(Succeed(), "expect to get ss "+key.Name+" in namespace "+key.Namespace)
					}, timeout, interval).Should(Succeed())
				}

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

					// with kube, deleting while initialising leads to long delays on terminating the namespace..

					By("verifying started")
					deployedCrd := brokerv1beta1.ActiveMQArtemis{}
					for _, createdCr := range createdCrs {

						key := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}

						Eventually(func(g Gomega) {
							g.Expect(k8sClient.Get(ctx, key, &deployedCrd)).Should(Succeed())
							g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
						}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
					}
				}

				By("clean up")
				for _, createdCr := range createdCrs {
					CleanResource(createdCr, createdCr.Name, createdCr.Namespace)
				}
			})
		})

		It("test watching all namespaces with same cr name and address creation", func() {

			brokerName := "bb-for-many-ns"
			testWatchNamespace("all", Default, func(g Gomega) {
				By("deploying same broker cr name to three namespaces")
				nameSpaces := []string{namespace1, namespace2, namespace3}
				var createdCrs []*brokerv1beta1.ActiveMQArtemis
				for _, ns := range nameSpaces {
					_, createdCr := DeployCustomBroker(ns, func(c *brokerv1beta1.ActiveMQArtemis) {
						c.Name = brokerName
					})
					createdCrs = append(createdCrs, createdCr)
				}

				By("Deploying Address to BB in ns3")
				addressName := "AnA"
				addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
				addressCrd.SetName("address-" + randString())
				addressCrd.SetNamespace(namespace3)
				addressCrd.Spec.AddressName = addressName
				addressCrd.Spec.QueueName = &addressName
				routingTypeShouldBeOptional := "anycast"
				addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional

				Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

				By("Verifying Address on BB in ns3")

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

					deployedCrd := brokerv1beta1.ActiveMQArtemis{}

					for _, ns := range nameSpaces {

						key := types.NamespacedName{Name: brokerName, Namespace: ns}

						By("asserting brokers ready")
						Eventually(func(g Gomega) {
							g.Expect(k8sClient.Get(ctx, key, &deployedCrd)).Should(Succeed())

							g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
							g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

						}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
					}

					By("asserting address exists on single ns3")
					podOrdinal := strconv.FormatInt(0, 10)
					podName := namer.CrToSS(brokerName) + "-" + podOrdinal

					By("checking creation in ns3")
					CheckQueueExistInPod(brokerName, podName, *addressCrd.Spec.QueueName, namespace3)

					By("checking no creation in ns1 and ns2")
					CheckQueueNotExistInPod(brokerName, podName, *addressCrd.Spec.QueueName, namespace2)
					CheckQueueNotExistInPod(brokerName, podName, *addressCrd.Spec.QueueName, namespace1)
				}

				By("clean up")
				for _, createdCr := range createdCrs {
					CleanResource(createdCr, createdCr.Name, createdCr.Namespace)
				}
			})
		})

		It("test watching all namespaces with same cr name and exposed console", func() {

			brokerName := "ec-for-many-ns"
			testWatchNamespace("all", Default, func(g Gomega) {
				By("deploying same broker cr name to three namespaces")
				nameSpaces := []string{namespace1, namespace2, namespace3}
				var createdCrs []*brokerv1beta1.ActiveMQArtemis
				for _, ns := range nameSpaces {
					_, createdCr := DeployCustomBroker(ns, func(c *brokerv1beta1.ActiveMQArtemis) {
						c.Name = brokerName
						c.Spec.Console.Expose = true

						if !isOpenshift {
							c.Spec.IngressDomain = defaultTestIngressDomain
						}
					})
					createdCrs = append(createdCrs, createdCr)
				}

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

					deployedCrd := brokerv1beta1.ActiveMQArtemis{}

					for _, ns := range nameSpaces {

						key := types.NamespacedName{Name: brokerName, Namespace: ns}

						By("asserting brokers ready")
						Eventually(func(g Gomega) {
							g.Expect(k8sClient.Get(ctx, key, &deployedCrd)).Should(Succeed())

							g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeTrue())
							g.Expect(meta.IsStatusConditionTrue(deployedCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

						}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
					}
				}

				By("clean up")
				for _, createdCr := range createdCrs {
					CleanResource(createdCr, createdCr.Name, createdCr.Namespace)
				}
			})
		})

		It("test watching multiple namespaces", Label("test-watching-namespace"), func() {
			testWatchNamespace("multiple", Default, func(g Gomega) {
				//only namespace2 and namespace3 is watched
				By("deploying broker in to all namespaces")
				var createdCrs []*brokerv1beta1.ActiveMQArtemis
				for _, ns := range []string{defaultNamespace, namespace1, namespace2, namespace3} {
					_, createdCr := DeployCustomBroker(ns, nil)
					createdCrs = append(createdCrs, createdCr)
				}

				By("check statefulset get created only in " + namespace2 + " and " + namespace3)
				for _, createdCr := range createdCrs {
					createdSs := &appsv1.StatefulSet{}
					key := types.NamespacedName{Name: namer.CrToSS(createdCr.Name), Namespace: createdCr.Namespace}
					if createdCr.Name == namespace2 || createdCr.Name == namespace3 {
						Eventually(func(g Gomega) {
							err := k8sClient.Get(ctx, key, createdSs)
							g.Expect(err).To(Succeed(), "expect to get ss "+key.Name+" in namespace "+key.Namespace)
						}, timeout, interval).Should(Succeed())

						if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

							// with kube, deleting while initialising leads to long delays on terminating the namespace..

							By("verifying started")
							deployedCrd := brokerv1beta1.ActiveMQArtemis{}

							crKey := types.NamespacedName{Name: createdCr.Name, Namespace: createdCr.Namespace}

							Eventually(func(g Gomega) {
								g.Expect(k8sClient.Get(ctx, crKey, &deployedCrd)).Should(Succeed())
								g.Expect(len(deployedCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(1))
							}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
						}

					} else {
						Eventually(func() bool {
							err := k8sClient.Get(ctx, key, createdSs)
							return errors.IsNotFound(err)
						}, timeout, interval).Should(BeTrue(), "statefulset shouldn't be in namespace "+createdCr.Namespace)
					}
				}

				By("clean up")
				for _, createdCr := range createdCrs {
					CleanResource(createdCr, createdCr.Name, createdCr.Namespace)
				}
			})
		})
	})
})

func createNamespace(namespace string, securityPolicy *string) error {
	ns := corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Namespace",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	if securityPolicy != nil {
		ns.ObjectMeta.Labels = map[string]string{
			"pod-security.kubernetes.io/audit":   *securityPolicy,
			"pod-security.kubernetes.io/enforce": *securityPolicy,
			"pod-security.kubernetes.io/warn":    *securityPolicy,
		}
	}

	err := k8sClient.Create(ctx, &ns, &client.CreateOptions{})

	// envTest won't delete, get stuck in Terminating state
	// https://github.com/kubernetes-sigs/controller-runtime/issues/880
	if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
		if errors.IsAlreadyExists(err) {
			// hense the ns may exist as we will only delete for USE_EXISTING_CLUSTER
			err = nil
		}
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

	g.Expect(k8sClient.Get(ctx, key, &ns)).Should(Succeed())

	zeroGracePeriodSeconds := int64(0) // immediate delete
	g.Expect(k8sClient.Delete(ctx, &ns, &client.DeleteOptions{GracePeriodSeconds: &zeroGracePeriodSeconds})).To(Succeed())

	if !wait {
		return
	}

	By("verifying gone: " + namespace)
	g.Eventually(func(g Gomega) {
		// verify gone
		err := k8sClient.Get(ctx, key, &ns)
		if err == nil && verbose {
			fmt.Printf("\nNamespace %s Status: %v\n", namespace, ns.Status)
			fmt.Printf("\nNamespace %s Spec: %v\n", namespace, ns)

		}
		g.Expect(err).ShouldNot(BeNil())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

}

func testWatchNamespace(kind string, g Gomega, testFunc func(g Gomega)) {

	shutdownControllerManager()

	restrictedSecurityPolicy := "restricted"
	g.Expect(createNamespace(namespace1, nil)).To(Succeed())
	g.Expect(createNamespace(namespace2, nil)).To(Succeed())
	g.Expect(createNamespace(namespace3, nil)).To(Succeed())
	g.Expect(createNamespace(restrictedNamespace, &restrictedSecurityPolicy)).To(Succeed())

	switch kind {
	case "single":
		createControllerManager(true, defaultNamespace)
	case "restricted":
		createControllerManager(true, restrictedNamespace)
	case "all":
		createControllerManager(true, "")
	default:
		createControllerManager(true, namespace2+","+namespace3)
	}

	testFunc(g)

	shutdownControllerManager()

	deleteNamespace(namespace1, true, g)
	deleteNamespace(namespace2, true, g)
	deleteNamespace(namespace3, true, g)
	deleteNamespace(restrictedNamespace, true, g)

	createControllerManagerForSuite()
}
