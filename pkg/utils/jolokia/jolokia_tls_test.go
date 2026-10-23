package jolokia

import (
	"crypto/tls"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

var _ = Describe("TLS profile caching", func() {

	AfterEach(func() {
		cachedTLSProfileMu.Lock()
		tlsProfileSet = false
		cachedTLSProfileMu.Unlock()
	})

	Describe("getTLSProfile", func() {
		It("returns the default profile when no profile has been set", func() {
			profile := getTLSProfile()
			Expect(profile.MinTLSVersion).To(Equal(openshifttls.DefaultMinTLSVersion))
			Expect(profile.Ciphers).To(Equal(openshifttls.DefaultTLSCiphers))
		})

		It("returns the profile that was set via SetTLSProfile", func() {
			custom := configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS13,
				Ciphers:       []string{"ECDHE-ECDSA-AES128-GCM-SHA256"},
			}
			SetTLSProfile(custom)

			profile := getTLSProfile()
			Expect(profile.MinTLSVersion).To(Equal(configv1.VersionTLS13))
			Expect(profile.Ciphers).To(ConsistOf("ECDHE-ECDSA-AES128-GCM-SHA256"))
		})
	})

	Describe("GetClientWithTimeout", func() {
		BeforeEach(func() {
			common.ResetOperatorCertCache()
		})

		It("does not set TLS config for http protocol", func() {
			j := &Jolokia{protocol: "http", client: fake.NewClientBuilder().Build()}
			httpClient := j.GetClientWithTimeout(time.Second)

			transport := httpClient.Transport.(*http.Transport)
			Expect(transport.TLSClientConfig).To(BeNil())
		})

		It("applies the cached TLS profile to the transport for https protocol", func() {
			SetTLSProfile(configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES128-GCM-SHA256",
					"ECDHE-ECDSA-AES128-GCM-SHA256",
				},
			})

			j := &Jolokia{
				ip:       "10.0.0.1",
				protocol: "https",
				client:   fake.NewClientBuilder().Build(),
			}
			httpClient := j.GetClientWithTimeout(time.Second)

			transport := httpClient.Transport.(*http.Transport)
			Expect(transport.TLSClientConfig).NotTo(BeNil())
			Expect(transport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
			Expect(transport.TLSClientConfig.CipherSuites).To(ContainElement(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256))
			Expect(transport.TLSClientConfig.CipherSuites).To(ContainElement(tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256))
		})

		It("sets ServerName from the Jolokia IP", func() {
			SetTLSProfile(configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			})

			j := &Jolokia{
				ip:       "broker-pod.broker-ns.svc",
				protocol: "https",
				client:   fake.NewClientBuilder().Build(),
			}
			httpClient := j.GetClientWithTimeout(time.Second)

			transport := httpClient.Transport.(*http.Transport)
			Expect(transport.TLSClientConfig.ServerName).To(Equal("broker-pod.broker-ns.svc"))
		})

		It("applies Modern (TLS 1.3) profile with no configurable cipher suites", func() {
			SetTLSProfile(configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS13,
			})

			j := &Jolokia{
				ip:       "10.0.0.1",
				protocol: "https",
				client:   fake.NewClientBuilder().Build(),
			}
			httpClient := j.GetClientWithTimeout(time.Second)

			transport := httpClient.Transport.(*http.Transport)
			Expect(transport.TLSClientConfig).NotTo(BeNil())
			Expect(transport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
		})

		It("uses the default profile when none has been set", func() {
			j := &Jolokia{
				ip:       "10.0.0.1",
				protocol: "https",
				client:   fake.NewClientBuilder().Build(),
			}
			httpClient := j.GetClientWithTimeout(time.Second)

			transport := httpClient.Transport.(*http.Transport)
			Expect(transport.TLSClientConfig).NotTo(BeNil())
			Expect(transport.TLSClientConfig.MinVersion).NotTo(BeZero())
		})
	})
})
