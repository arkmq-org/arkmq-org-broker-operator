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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

// Scale ceiling: 150 apps is the practical limit of a single {svc}-certs
// secret (~6KB per app × 150 ≈ 900KB, within the 1MB Kubernetes limit).
// This constant is documented in pkg/chain-of-trust/types.go.
const scaleTestAppCount = cot.MaxAppsPerCertsSecret - 1 // 149 bulk apps + 1 primary = 150

var _ = Describe("chain-of-trust scale ceiling", Label("chain-of-trust", "scale-ceiling"), func() {

	var installedCertManager bool

	BeforeEach(func() {
		BeforeEachSpec()

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			common.ResetOperatorCertCache()
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
				installedCertManager = true
			}
		}
	})

	AfterEach(func() {
		if false && os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}
		AfterEachSpec()
	})

	It("2-app smoke: service + primary + 1 bulk app with messaging and metrics", func() {

		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		primaryApp := "primary-app"

		By("creating BrokerService")
		svcCrd := broker.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"env": "scale-smoke"},
			},
			Spec: broker.BrokerServiceSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &svcCrd)).Should(Succeed())

		serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, serviceKey, svc)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(svc.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			b := &broker.Broker{}
			g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deploying primary BrokerApp")
		app0 := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      primaryApp,
				Namespace: defaultNamespace,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "scale-smoke"},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{{Address: "SMOKE.QUEUE"}},
						ConsumerOf: []broker.AddressRef{{Address: "SMOKE.QUEUE"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &app0)).Should(Succeed())

		var bindingSecretName string
		appKey := types.NamespacedName{Name: primaryApp, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			a := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, appKey, a)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			g.Expect(a.Status.Service).ShouldNot(BeNil())
			bindingSecretName = a.Status.Service.Secret
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		GinkgoWriter.Printf("[smoke] primary app Ready, binding secret: %s\n", bindingSecretName)

		Eventually(func(g Gomega) {
			b := &broker.Broker{}
			g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deploying continuous producer")
		appCertName := cot.AppCertName(primaryApp)
		appCATrustName := cot.AppCATrustSecretName(primaryApp)
		brokerImage := version.LatestKubeImage

		pemcfgSecretName := "cert-pemcfg-smoke"
		pemcfgKey := types.NamespacedName{Name: pemcfgSecretName, Namespace: defaultNamespace}
		pemcfgSecret := secrets.NewSecret(pemcfgKey, map[string][]byte{
			"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
			"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
		}, nil)
		Expect(k8sClient.Create(ctx, pemcfgSecret, &client.CreateOptions{})).Should(Succeed())

		serviceHostEnvVar := "BROKER_SERVICE_HOST"
		servicePortEnvVar := "BROKER_SERVICE_PORT"
		buf := &bytes.Buffer{}
		fmt.Fprintf(buf, "amqps://${%s}:${%s}", serviceHostEnvVar, servicePortEnvVar)
		fmt.Fprintf(buf, "?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/tls.crt")
		fmt.Fprintf(buf, "\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg")
		serviceUrl := buf.String()

		boolFalse := false
		var replicas int32 = 1
		producerCmd := fmt.Sprintf(
			"while true; do java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url %s --message-count 1 --destination queue://SMOKE.QUEUE; sleep 2; done",
			serviceUrl)
		producerDeploy := &appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "smoke-producer", Namespace: defaultNamespace},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "smoke-producer"}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"role": "smoke-producer"}},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:    "producer",
							Image:   brokerImage,
							Command: []string{"/bin/sh", "-c", producerCmd},
							Env: []corev1.EnvVar{
								{Name: "JDK_JAVA_OPTIONS", Value: "-Djava.security.properties=/app/tls/pem/java.security"},
								{Name: serviceHostEnvVar, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: bindingSecretName}, Key: "host", Optional: &boolFalse}}},
								{Name: servicePortEnvVar, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: bindingSecretName}, Key: "port", Optional: &boolFalse}}},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "trust", MountPath: "/app/tls/ca"},
								{Name: "cert", MountPath: "/app/tls/client"},
								{Name: "pem", MountPath: "/app/tls/pem"},
							},
						}},
						Volumes: []corev1.Volume{
							{Name: "trust", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: appCATrustName}}},
							{Name: "cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: appCertName}}},
							{Name: "pem", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: pemcfgSecretName}}},
						},
						RestartPolicy: corev1.RestartPolicyAlways,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, producerDeploy)).Should(Succeed())

		Eventually(func(g Gomega) {
			d := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "smoke-producer", Namespace: defaultNamespace}, d)).Should(Succeed())
			g.Expect(d.Status.ReadyReplicas).Should(BeNumerically(">=", 1))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		GinkgoWriter.Printf("[smoke] producer running\n")

		By("scraping metrics using metrics-plane certs")
		promCertSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.PrometheusCertName(serviceName), Namespace: defaultNamespace,
		}, promCertSecret)).Should(Succeed())
		metricsCASecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.MetricsRootCertSecretName(serviceName), Namespace: defaultNamespace,
		}, metricsCASecret)).Should(Succeed())
		promCert, err := tls.X509KeyPair(promCertSecret.Data["tls.crt"], promCertSecret.Data["tls.key"])
		Expect(err).Should(Succeed())
		metricsCAPool := x509.NewCertPool()
		Expect(metricsCAPool.AppendCertsFromPEM(metricsCASecret.Data["tls.crt"])).Should(BeTrue())
		serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

		Eventually(func(g Gomega) {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.TLSClientConfig = &tls.Config{
				ServerName:   serverName,
				Certificates: []tls.Certificate{promCert},
				RootCAs:      metricsCAPool,
			}
			httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
			g.Expect(err).Should(Succeed())
			defer func() { _ = resp.Body.Close() }()
			g.Expect(resp.StatusCode).Should(Equal(http.StatusOK))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		GinkgoWriter.Printf("[smoke] metrics scrape OK\n")

		By("provisioning 1 bulk app while producer is running")
		bulkApp := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bulk-app-000",
				Namespace: defaultNamespace,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "scale-smoke"},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{{Address: "BULK.QUEUE.000"}},
						ConsumerOf: []broker.AddressRef{{Address: "BULK.QUEUE.000"}},
					},
				},
			},
		}
		start := time.Now()
		Expect(k8sClient.Create(ctx, &bulkApp)).Should(Succeed())

		bulkAppKey := types.NamespacedName{Name: "bulk-app-000", Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			a := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, bulkAppKey, a)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		GinkgoWriter.Printf("[smoke] bulk app provisioned in %v\n", time.Since(start))

		By("verifying producer survived bulk app provisioning")
		Eventually(func(g Gomega) {
			d := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "smoke-producer", Namespace: defaultNamespace}, d)).Should(Succeed())
			g.Expect(d.Status.ReadyReplicas).Should(BeNumerically(">=", 1))
		}, 30*time.Second, 2*time.Second).Should(Succeed())

		By("verifying metrics still work after bulk app provisioning")
		Eventually(func(g Gomega) {
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.TLSClientConfig = &tls.Config{
				ServerName:   serverName,
				Certificates: []tls.Certificate{promCert},
				RootCAs:      metricsCAPool,
			}
			httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
			g.Expect(err).Should(Succeed())
			defer func() { _ = resp.Body.Close() }()
			g.Expect(resp.StatusCode).Should(Equal(http.StatusOK))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting bulk app and verifying producer survives")
		Expect(k8sClient.Delete(ctx, &bulkApp)).Should(Succeed())
		Eventually(func(g Gomega) {
			a := &broker.BrokerApp{}
			err := k8sClient.Get(ctx, bulkAppKey, a)
			g.Expect(err).Should(HaveOccurred())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			d := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "smoke-producer", Namespace: defaultNamespace}, d)).Should(Succeed())
			g.Expect(d.Status.ReadyReplicas).Should(BeNumerically(">=", 1))
		}, 30*time.Second, 2*time.Second).Should(Succeed())
		GinkgoWriter.Printf("[smoke] 2-app smoke test PASSED\n")

		By("cleanup")
		Expect(k8sClient.Delete(ctx, producerDeploy)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, &app0)).Should(Succeed())
		cascade := metav1.DeletePropagationForeground
		Expect(k8sClient.Delete(ctx, &svcCrd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			err := k8sClient.Get(ctx, serviceKey, svc)
			g.Expect(err).Should(HaveOccurred())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("150-app ceiling: no broker restart during bulk provisioning and deprovisioning", Label("verySlow"), func() {

		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		primaryApp := "primary-app"

		// ====================================================================
		// Phase 1: Deploy service + primary app
		// ====================================================================

		By("creating BrokerService")
		svcCrd := broker.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"env": "scale-ceiling"},
			},
			Spec: broker.BrokerServiceSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &svcCrd)).Should(Succeed())

		serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, serviceKey, svc)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(svc.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			b := &broker.Broker{}
			g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deploying primary BrokerApp")
		app0 := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      primaryApp,
				Namespace: defaultNamespace,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "scale-ceiling"},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{{Address: "SCALE.QUEUE"}},
						ConsumerOf: []broker.AddressRef{{Address: "SCALE.QUEUE"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &app0)).Should(Succeed())

		appKey := types.NamespacedName{Name: primaryApp, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			a := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, appKey, a)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			b := &broker.Broker{}
			g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		// ====================================================================
		// Phase 2: Record broker pod baseline restart count
		// ====================================================================

		brokerPodName := serviceName + "-ss-0"
		brokerPodKey := types.NamespacedName{Name: brokerPodName, Namespace: defaultNamespace}

		getBrokerRestartCount := func() int32 {
			pod := &corev1.Pod{}
			ExpectWithOffset(1, k8sClient.Get(ctx, brokerPodKey, pod)).Should(Succeed())
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == serviceName+"-container" || cs.Name == "broker" {
					return cs.RestartCount
				}
			}
			if len(pod.Status.ContainerStatuses) > 0 {
				return pod.Status.ContainerStatuses[0].RestartCount
			}
			return 0
		}

		baselineRestarts := getBrokerRestartCount()
		GinkgoWriter.Printf("[scale] broker pod %s baseline restart count: %d\n", brokerPodName, baselineRestarts)

		// ====================================================================
		// Phase 3: Scrape metrics (metrics plane)
		// ====================================================================

		By("scraping metrics for the service using metrics-plane certs")
		promCertSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.PrometheusCertName(serviceName), Namespace: defaultNamespace,
		}, promCertSecret)).Should(Succeed())

		metricsCASecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.MetricsRootCertSecretName(serviceName), Namespace: defaultNamespace,
		}, metricsCASecret)).Should(Succeed())

		promCert, err := tls.X509KeyPair(promCertSecret.Data["tls.crt"], promCertSecret.Data["tls.key"])
		Expect(err).Should(Succeed())

		metricsCAPool := x509.NewCertPool()
		Expect(metricsCAPool.AppendCertsFromPEM(metricsCASecret.Data["tls.crt"])).Should(BeTrue())

		serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

		scrapeMetrics := func() {
			Eventually(func(g Gomega) {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.TLSClientConfig = &tls.Config{
					ServerName:   serverName,
					Certificates: []tls.Certificate{promCert},
					RootCAs:      metricsCAPool,
				}
				httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}
				resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
				g.Expect(err).Should(Succeed())
				defer func() { _ = resp.Body.Close() }()
				g.Expect(resp.StatusCode).Should(Equal(http.StatusOK))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		}

		scrapeMetrics()
		GinkgoWriter.Printf("[metrics] service metrics scrape OK\n")

		// ====================================================================
		// Phase 4: Provision 149 more apps in batches — measure timing
		// ====================================================================

		const batchSize = 50
		numBatches := (scaleTestAppCount + batchSize - 1) / batchSize

		By(fmt.Sprintf("provisioning %d additional apps in batches of %d (total %d)", scaleTestAppCount, batchSize, scaleTestAppCount+1))
		batchTimes := make([]time.Duration, numBatches)
		bulkApps := make([]broker.BrokerApp, scaleTestAppCount)

		for b := 0; b < numBatches; b++ {
			batchStart := b * batchSize
			batchEnd := batchStart + batchSize
			if batchEnd > scaleTestAppCount {
				batchEnd = scaleTestAppCount
			}
			start := time.Now()

			for i := batchStart; i < batchEnd; i++ {
				appName := fmt.Sprintf("bulk-app-%03d", i)
				queueName := fmt.Sprintf("BULK.QUEUE.%03d", i)
				bulkApps[i] = broker.BrokerApp{
					TypeMeta: metav1.TypeMeta{
						Kind:       "BrokerApp",
						APIVersion: broker.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      appName,
						Namespace: defaultNamespace,
					},
					Spec: broker.BrokerAppSpec{
						ServiceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"env": "scale-ceiling"},
						},
						Capabilities: []broker.AppCapabilityType{
							{
								ProducerOf: []broker.AddressRef{{Address: queueName}},
								ConsumerOf: []broker.AddressRef{{Address: queueName}},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, &bulkApps[i])).Should(Succeed())
			}

			for i := batchStart; i < batchEnd; i++ {
				appKey := types.NamespacedName{Name: bulkApps[i].Name, Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					a := &broker.BrokerApp{}
					g.Expect(k8sClient.Get(ctx, appKey, a)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			batchTimes[b] = time.Since(start)
			GinkgoWriter.Printf("[scale] batch %d/%d (%d apps) provisioned in %v\n",
				b+1, numBatches, batchEnd-batchStart, batchTimes[b])
		}

		firstTime := batchTimes[0]
		lastTime := batchTimes[numBatches-1]
		GinkgoWriter.Printf("[scale] first batch: %v, last batch: %v, ratio: %.2fx\n",
			firstTime, lastTime, float64(lastTime)/float64(firstTime))

		By("verifying {svc}-certs secret exists and is within 1MB")
		certsSecretKey := types.NamespacedName{
			Name:      cot.AppCertsSecretName(serviceName),
			Namespace: defaultNamespace,
		}
		certsSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, certsSecretKey, certsSecret)).Should(Succeed())
		var totalBytes int
		for _, v := range certsSecret.Data {
			totalBytes += len(v)
		}
		GinkgoWriter.Printf("[scale] {svc}-certs secret data size: %d bytes (%d keys)\n", totalBytes, len(certsSecret.Data))
		Expect(totalBytes).Should(BeNumerically("<", 1048576), "cert secret must stay under 1MB")

		// ====================================================================
		// Phase 5: Assert zero broker restarts after bulk provisioning
		// ====================================================================

		By("asserting broker pod was NOT restarted during bulk provisioning")
		Expect(getBrokerRestartCount()).Should(Equal(baselineRestarts),
			"broker pod must not restart during app provisioning")

		By("scraping metrics after bulk provisioning")
		scrapeMetrics()
		GinkgoWriter.Printf("[metrics] service metrics scrape OK after bulk provisioning\n")

		// ====================================================================
		// Phase 6: Remove 149 bulk apps — assert zero restarts
		// ====================================================================

		By(fmt.Sprintf("deleting %d bulk apps", scaleTestAppCount))
		for i := 0; i < scaleTestAppCount; i++ {
			Expect(k8sClient.Delete(ctx, &bulkApps[i])).Should(Succeed())
		}

		By("waiting for all bulk apps to be fully deleted")
		Eventually(func(g Gomega) {
			for i := 0; i < scaleTestAppCount; i++ {
				appName := fmt.Sprintf("bulk-app-%03d", i)
				a := &broker.BrokerApp{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: defaultNamespace}, a)
				g.Expect(err).Should(HaveOccurred())
			}
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("asserting broker pod was NOT restarted during bulk deprovisioning")
		Expect(getBrokerRestartCount()).Should(Equal(baselineRestarts),
			"broker pod must not restart during app deprovisioning")

		By("scraping metrics after bulk deprovisioning")
		scrapeMetrics()
		GinkgoWriter.Printf("[metrics] service metrics scrape OK after bulk deprovisioning\n")

		By("verifying {svc}-certs secret shrank (only primary app data remains)")
		Expect(k8sClient.Get(ctx, certsSecretKey, certsSecret)).Should(Succeed())
		var shrunkBytes int
		for _, v := range certsSecret.Data {
			shrunkBytes += len(v)
		}
		GinkgoWriter.Printf("[scale] {svc}-certs after cleanup: %d bytes (%d keys)\n", shrunkBytes, len(certsSecret.Data))
		Expect(shrunkBytes).Should(BeNumerically("<", totalBytes),
			"cert secret should shrink after bulk deprovisioning")

		// ====================================================================
		// Summary
		// ====================================================================

		GinkgoWriter.Printf("\n=== Scale Ceiling Test Summary ===\n")
		GinkgoWriter.Printf("Total apps provisioned:     %d (ceiling: %d)\n", scaleTestAppCount+1, cot.MaxAppsPerCertsSecret)
		GinkgoWriter.Printf("Batch size:                 %d\n", batchSize)
		GinkgoWriter.Printf("First batch time:           %v\n", firstTime)
		GinkgoWriter.Printf("Last batch time:            %v\n", lastTime)
		GinkgoWriter.Printf("Ratio (last/first):         %.2fx\n", float64(lastTime)/float64(firstTime))
		GinkgoWriter.Printf("Peak cert secret size:      %d bytes\n", totalBytes)
		GinkgoWriter.Printf("Post-cleanup secret size:   %d bytes\n", shrunkBytes)
		GinkgoWriter.Printf("Broker restarts:            %d\n", getBrokerRestartCount()-baselineRestarts)
		GinkgoWriter.Printf("Metrics scraping survived:  YES\n")
		GinkgoWriter.Printf("=================================\n")

		// ====================================================================
		// Cleanup
		// ====================================================================

		By("cleanup: deleting primary app")
		Expect(k8sClient.Delete(ctx, &app0)).Should(Succeed())

		By("cleanup: deleting BrokerService")
		cascade := metav1.DeletePropagationForeground
		Expect(k8sClient.Delete(ctx, &svcCrd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			err := k8sClient.Get(ctx, serviceKey, svc)
			g.Expect(err).Should(HaveOccurred())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})
})
