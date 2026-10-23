package controllers

import (
	"context"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
)

var _ = Describe("TLS security profile integration", Label("tls-profile-test"), func() {

	var originalTLSProfile *configv1.TLSSecurityProfile

	saveAndRestoreTLSProfile := func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		apiServer := &configv1.APIServer{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "cluster"}, apiServer)).To(Succeed())
		originalTLSProfile = apiServer.Spec.TLSSecurityProfile
	}

	restoreTLSProfile := func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		apiServer := &configv1.APIServer{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "cluster"}, apiServer)).To(Succeed())
		apiServer.Spec.TLSSecurityProfile = originalTLSProfile
		Expect(k8sClient.Update(context.Background(), apiServer)).To(Succeed())
	}

	setClusterTLSProfile := func(profile *configv1.TLSSecurityProfile) {
		apiServer := &configv1.APIServer{}
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: "cluster"}, apiServer)).To(Succeed())
		apiServer.Spec.TLSSecurityProfile = profile
		Expect(k8sClient.Update(context.Background(), apiServer)).To(Succeed())
	}

	Context("broker inherits cluster TLS profile", func() {

		BeforeEach(func() {
			saveAndRestoreTLSProfile()
		})

		AfterEach(func() {
			restoreTLSProfile()
		})

		It("applies Intermediate profile ciphers when CR does not specify them", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("requires existing cluster")
			}

			By("ensuring cluster TLS profile is Intermediate")
			setClusterTLSProfile(&configv1.TLSSecurityProfile{
				Type:         configv1.TLSProfileIntermediateType,
				Intermediate: &configv1.IntermediateTLSProfile{},
			})

			By("creating a TLS secret")
			sslSecretName := "tls-profile-test-ssl-secret"
			sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", []string{"*"})
			Expect(err).To(BeNil())
			if err := k8sClient.Create(context.Background(), sslSecret); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}
			defer CleanResource(sslSecret, sslSecretName, defaultNamespace)

			By("deploying a broker with SSL acceptor and no cipher/protocol override")
			_, brokerCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.BrokerCluster) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.Acceptors = []brokerv1beta2.AcceptorType{
					{
						Name:       "tls-test",
						Port:       61617,
						Protocols:  "amqp,core",
						SSLEnabled: true,
						SSLSecret:  sslSecretName,
					},
				}
			})
			defer CleanResource(brokerCr, brokerCr.Name, brokerCr.Namespace)

			By("verifying the broker pod is running and acceptor started")
			WaitForPod(brokerCr.Name)
			podName := namer.CrToSSOrdinal(brokerCr.Name, 0)

			By("verifying Intermediate protocols are configured on the acceptor")
			Eventually(func(g Gomega) {
				result := ExecOnPod(podName, brokerCr.Name, defaultNamespace,
					[]string{"grep", "-o", "enabledProtocols=[^;]*", "/home/jboss/amq-broker/etc/broker.xml"}, g)
				g.Expect(result).To(ContainSubstring("TLSv1.2"))
				g.Expect(result).To(ContainSubstring("TLSv1.3"))
				g.Expect(result).NotTo(ContainSubstring("TLSv1.1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying Intermediate ciphers are configured on the acceptor")
			Eventually(func(g Gomega) {
				result := ExecOnPod(podName, brokerCr.Name, defaultNamespace,
					[]string{"grep", "-o", "enabledCipherSuites=[^;]*", "/home/jboss/amq-broker/etc/broker.xml"}, g)
				g.Expect(result).To(ContainSubstring("ECDHE-RSA-AES128-GCM-SHA256"))
				g.Expect(result).NotTo(ContainSubstring("DES-CBC3-SHA"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})

		It("applies Custom profile with restricted ciphers", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("requires existing cluster")
			}

			By("setting a custom TLS profile with only two ciphers")
			setClusterTLSProfile(&configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						Ciphers: []string{
							"ECDHE-ECDSA-AES128-GCM-SHA256",
							"ECDHE-RSA-AES128-GCM-SHA256",
						},
						MinTLSVersion: configv1.VersionTLS12,
					},
				},
			})

			By("creating a TLS secret")
			sslSecretName := "tls-profile-custom-ssl-secret"
			sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", []string{"*"})
			Expect(err).To(BeNil())
			if err := k8sClient.Create(context.Background(), sslSecret); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}
			defer CleanResource(sslSecret, sslSecretName, defaultNamespace)

			By("deploying a broker with SSL acceptor and no cipher/protocol override")
			_, brokerCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.BrokerCluster) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.Acceptors = []brokerv1beta2.AcceptorType{
					{
						Name:       "tls-custom",
						Port:       61617,
						Protocols:  "amqp,core",
						SSLEnabled: true,
						SSLSecret:  sslSecretName,
					},
				}
			})
			defer CleanResource(brokerCr, brokerCr.Name, brokerCr.Namespace)

			By("verifying the broker pod is running")
			WaitForPod(brokerCr.Name)
			podName := namer.CrToSSOrdinal(brokerCr.Name, 0)

			By("verifying only the custom ciphers are configured")
			Eventually(func(g Gomega) {
				result := ExecOnPod(podName, brokerCr.Name, defaultNamespace,
					[]string{"grep", "-o", "enabledCipherSuites=[^;]*", "/home/jboss/amq-broker/etc/broker.xml"}, g)
				g.Expect(result).To(ContainSubstring("ECDHE-ECDSA-AES128-GCM-SHA256"))
				g.Expect(result).To(ContainSubstring("ECDHE-RSA-AES128-GCM-SHA256"))
				g.Expect(result).NotTo(ContainSubstring("AES256"))
				g.Expect(result).NotTo(ContainSubstring("CHACHA20"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying TLSv1.2 and TLSv1.3 protocols are configured")
			Eventually(func(g Gomega) {
				result := ExecOnPod(podName, brokerCr.Name, defaultNamespace,
					[]string{"grep", "-o", "enabledProtocols=[^;]*", "/home/jboss/amq-broker/etc/broker.xml"}, g)
				g.Expect(result).To(ContainSubstring("TLSv1.2"))
				g.Expect(result).To(ContainSubstring("TLSv1.3"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})

		It("CR cipher/protocol fields override the cluster profile", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("requires existing cluster")
			}

			By("ensuring cluster TLS profile is Intermediate")
			setClusterTLSProfile(&configv1.TLSSecurityProfile{
				Type:         configv1.TLSProfileIntermediateType,
				Intermediate: &configv1.IntermediateTLSProfile{},
			})

			By("creating a TLS secret")
			sslSecretName := "tls-profile-override-ssl-secret"
			sslSecret, err := CreateTlsSecret(sslSecretName, defaultNamespace, "password", []string{"*"})
			Expect(err).To(BeNil())
			if err := k8sClient.Create(context.Background(), sslSecret); err != nil {
				Expect(errors.IsAlreadyExists(err)).To(BeTrue())
			}
			defer CleanResource(sslSecret, sslSecretName, defaultNamespace)

			By("deploying a broker with explicit cipher suites in the CR")
			crCiphers := "ECDHE-RSA-AES256-GCM-SHA384"
			crProtocols := "TLSv1.3"
			_, brokerCr := DeployCustomBrokerV1(defaultNamespace, func(candidate *brokerv1beta2.BrokerCluster) {
				candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				candidate.Spec.Acceptors = []brokerv1beta2.AcceptorType{
					{
						Name:                "tls-override",
						Port:                61617,
						Protocols:           "amqp,core",
						SSLEnabled:          true,
						SSLSecret:           sslSecretName,
						EnabledCipherSuites: crCiphers,
						EnabledProtocols:    crProtocols,
					},
				}
			})
			defer CleanResource(brokerCr, brokerCr.Name, brokerCr.Namespace)

			By("verifying the broker pod is running")
			WaitForPod(brokerCr.Name)
			podName := namer.CrToSSOrdinal(brokerCr.Name, 0)

			By("verifying the CR-specified ciphers are used, not the cluster profile")
			Eventually(func(g Gomega) {
				result := ExecOnPod(podName, brokerCr.Name, defaultNamespace,
					[]string{"grep", "-o", "enabledCipherSuites=[^;]*", "/home/jboss/amq-broker/etc/broker.xml"}, g)
				g.Expect(result).To(ContainSubstring(crCiphers))
				g.Expect(result).NotTo(ContainSubstring("ECDHE-ECDSA-AES128-GCM-SHA256"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying the CR-specified protocols are used")
			Eventually(func(g Gomega) {
				result := ExecOnPod(podName, brokerCr.Name, defaultNamespace,
					[]string{"grep", "-o", "enabledProtocols=[^;]*", "/home/jboss/amq-broker/etc/broker.xml"}, g)
				g.Expect(result).To(Equal("enabledProtocols=" + crProtocols))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})
})
