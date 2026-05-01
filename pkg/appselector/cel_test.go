package appselector

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("CEL Expressions", func() {
	type testCase struct {
		expr      string
		app       *v1beta2.BrokerApp
		service   *v1beta2.BrokerService
		expected  bool
		shouldErr bool
	}

	DescribeTable("evaluates expressions correctly",
		func(tc testCase) {
			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

			namespaces := []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: v1.ObjectMeta{
						Name: tc.app.Namespace,
					},
				},
			}
			if tc.service.Namespace != tc.app.Namespace {
				namespaces = append(namespaces, &corev1.Namespace{
					ObjectMeta: v1.ObjectMeta{
						Name: tc.service.Namespace,
					},
				})
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(namespaces...).Build()

			result, err := evaluateExpression(tc.expr, tc.app, tc.service, fakeClient)

			if tc.shouldErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(tc.expected))
		},
		Entry("appNamespace name access",
			testCase{
				expr: `appNamespace.metadata.name == "team-a"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("serviceNamespace name access",
			testCase{
				expr: `serviceNamespace.metadata.name == "shared"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("both namespaces different",
			testCase{
				expr: `appNamespace.metadata.name != serviceNamespace.metadata.name`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("both namespaces same",
			testCase{
				expr: `appNamespace.metadata.name == serviceNamespace.metadata.name`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "shared",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("exact namespace match",
			testCase{
				expr: `app.metadata.namespace == "team-a"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("exact namespace mismatch",
			testCase{
				expr: `app.metadata.namespace == "team-a"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-b",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: false,
			}),
		Entry("same namespace",
			testCase{
				expr: "app.metadata.namespace == service.metadata.namespace",
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "shared",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("different namespace",
			testCase{
				expr: "app.metadata.namespace == service.metadata.namespace",
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "other",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: false,
			}),
		Entry("true always",
			testCase{
				expr: "true",
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "anything",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "any-broker",
						Namespace: "anything",
					},
				},
				expected: true,
			}),
		Entry("namespace in list",
			testCase{
				expr: `app.metadata.namespace in ["team-a", "team-b", "team-c"]`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("namespace not in list",
			testCase{
				expr: `app.metadata.namespace in ["team-a", "team-b", "team-c"]`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-d",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: false,
			}),
		Entry("namespace startsWith",
			testCase{
				expr: `app.metadata.namespace.startsWith("team-")`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a-prod",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("namespace endsWith",
			testCase{
				expr: `app.metadata.namespace.endsWith("-prod")`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a-prod",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("namespace prefix and suffix",
			testCase{
				expr: `app.metadata.namespace.startsWith("team-") && app.metadata.namespace.endsWith("-prod")`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a-prod",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("app name startsWith prod",
			testCase{
				expr: `app.metadata.name.startsWith("prod-")`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "prod-payment-service",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("app name does not start with prod",
			testCase{
				expr: `app.metadata.name.startsWith("prod-")`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "test-payment-service",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: false,
			}),
		Entry("port-based authorization",
			testCase{
				expr: `app.status.service.assignedPort >= 62000`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "dev",
					},
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 62100,
						},
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("port too low",
			testCase{
				expr: `app.status.service.assignedPort >= 62000`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "dev",
					},
					Status: v1beta2.BrokerAppStatus{
						Service: &v1beta2.BrokerServiceBindingStatus{
							AssignedPort: 61616,
						},
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: false,
			}),
		Entry("label-based authorization",
			testCase{
				expr: `has(app.metadata.labels) && app.metadata.labels["tier"] == "premium"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
						Labels: map[string]string{
							"tier": "premium",
						},
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: true,
			}),
		Entry("missing label",
			testCase{
				expr: `has(app.metadata.labels) && app.metadata.labels["tier"] == "premium"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "team-a",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "shared",
					},
				},
				expected: false,
			}),
	)

	DescribeTable("evaluates expressions correctly with namespace metadata",
		func(tc testCase) {
			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

			// Create namespaces with labels/annotations
			appNs := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name:        tc.app.Namespace,
					Labels:      map[string]string{"tier": "premium", "env": "prod"},
					Annotations: map[string]string{"approved": "true", "team": "alpha"},
				},
			}
			svcNs := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name:        tc.service.Namespace,
					Labels:      map[string]string{"tier": "shared"},
					Annotations: map[string]string{"managed": "true"},
				},
			}

			namespaces := []runtime.Object{appNs}
			if tc.service.Namespace != tc.app.Namespace {
				namespaces = append(namespaces, svcNs)
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(namespaces...).Build()

			result, err := evaluateExpression(tc.expr, tc.app, tc.service, fakeClient)

			if tc.shouldErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(tc.expected))
		},
		Entry("namespace label access",
			testCase{
				expr: `has(appNamespace.metadata.labels) && appNamespace.metadata.labels["tier"] == "premium"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "app-ns",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "svc-ns",
					},
				},
				expected: true,
			}),
		Entry("namespace annotation access",
			testCase{
				expr: `has(appNamespace.metadata.annotations) && appNamespace.metadata.annotations["approved"] == "true"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "app-ns",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "svc-ns",
					},
				},
				expected: true,
			}),
		Entry("service namespace label access",
			testCase{
				expr: `has(serviceNamespace.metadata.labels) && serviceNamespace.metadata.labels["tier"] == "shared"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "app-ns",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "svc-ns",
					},
				},
				expected: true,
			}),
		Entry("namespace label mismatch",
			testCase{
				expr: `has(appNamespace.metadata.labels) && appNamespace.metadata.labels["tier"] == "standard"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "app-ns",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "svc-ns",
					},
				},
				expected: false,
			}),
		Entry("matching namespace labels between app and service",
			testCase{
				expr: `has(appNamespace.metadata.labels) && has(serviceNamespace.metadata.labels) && appNamespace.metadata.labels["env"] == "prod"`,
				app: &v1beta2.BrokerApp{
					ObjectMeta: v1.ObjectMeta{
						Name:      "myapp",
						Namespace: "app-ns",
					},
				},
				service: &v1beta2.BrokerService{
					ObjectMeta: v1.ObjectMeta{
						Name:      "shared-broker",
						Namespace: "svc-ns",
					},
				},
				expected: true,
			}),
	)
})

var _ = Describe("Namespace Not Found Fails Closed", func() {
	type testCase struct {
		expr      string
		createApp bool
		createSvc bool
	}

	DescribeTable("fails when namespace cannot be fetched",
		func(tc testCase) {
			app := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-app",
					Namespace: "team-a",
				},
			}

			service := &v1beta2.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      "test-service",
					Namespace: "shared",
				},
			}

			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

			var namespaces []runtime.Object
			if tc.createApp {
				namespaces = append(namespaces, &corev1.Namespace{
					ObjectMeta: v1.ObjectMeta{Name: "team-a"},
				})
			}
			if tc.createSvc {
				namespaces = append(namespaces, &corev1.Namespace{
					ObjectMeta: v1.ObjectMeta{Name: "shared"},
				})
			}

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(namespaces...).Build()

			result, err := evaluateExpression(tc.expr, app, service, fakeClient)

			Expect(err).To(HaveOccurred())
			Expect(result).To(BeFalse())
		},
		Entry("appNamespace not found",
			testCase{
				expr:      `appNamespace.metadata.name == "team-a"`,
				createApp: false,
				createSvc: true,
			}),
		Entry("serviceNamespace not found",
			testCase{
				expr:      `serviceNamespace.metadata.name == "shared"`,
				createApp: true,
				createSvc: false,
			}),
		Entry("both namespaces not found",
			testCase{
				expr:      `appNamespace.metadata.name == serviceNamespace.metadata.name`,
				createApp: false,
				createSvc: false,
			}),
	)
})

var _ = Describe("Invalid CEL Expressions", func() {
	var (
		app        *v1beta2.BrokerApp
		service    *v1beta2.BrokerService
		fakeClient client.Client
	)

	BeforeEach(func() {
		app = &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-app",
				Namespace: "test-ns",
			},
		}
		service = &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test-ns",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(&corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: "test-ns"},
			}).
			Build()
	})

	It("returns error for syntax error in expression", func() {
		expr := `app.metadata.namespace ==`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to compile CEL expression"))
		Expect(result).To(BeFalse())
	})

	It("returns error for non-boolean return type (string)", func() {
		expr := `app.metadata.namespace`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must return boolean"))
		Expect(result).To(BeFalse())
	})

	It("returns error for non-boolean return type (int)", func() {
		expr := `app.spec.acceptor.port`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must return boolean"))
		Expect(result).To(BeFalse())
	})

	It("returns error for invalid field reference", func() {
		expr := `app.nonexistent.field == "value"`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("returns error for type mismatch", func() {
		expr := `app.metadata.namespace + 123`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("Nil Input Validation", func() {
	var (
		app        *v1beta2.BrokerApp
		service    *v1beta2.BrokerService
		fakeClient client.Client
	)

	BeforeEach(func() {
		app = &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-app",
				Namespace: "test-ns",
			},
		}
		service = &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test-ns",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(&corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: "test-ns"},
			}).
			Build()
	})

	It("returns error for nil app", func() {
		expr := `app.metadata.namespace == "test"`
		result, err := evaluateExpression(expr, nil, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must not be nil"))
		Expect(result).To(BeFalse())
	})

	It("returns error for nil service", func() {
		expr := `app.metadata.namespace == "test"`
		result, err := evaluateExpression(expr, app, nil, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must not be nil"))
		Expect(result).To(BeFalse())
	})

	It("returns error for nil client", func() {
		expr := `app.metadata.namespace == "test"`
		result, err := evaluateExpression(expr, app, service, nil)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must not be nil"))
		Expect(result).To(BeFalse())
	})

	It("returns error for empty expression", func() {
		result, err := evaluateExpression("", app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("expression cannot be empty"))
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("Matches() Wrapper Function", func() {
	var (
		app        *v1beta2.BrokerApp
		service    *v1beta2.BrokerService
		fakeClient client.Client
	)

	BeforeEach(func() {
		app = &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-app",
				Namespace: "test-ns",
			},
		}
		service = &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-service",
				Namespace: "test-ns",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(&corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: "test-ns"},
			}).
			Build()
	})

	It("uses default expression when appSelectorExpression is empty", func() {
		service.Spec.AppSelectorExpression = ""
		result, err := Matches(app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue()) // Same namespace
	})

	It("uses custom expression when provided", func() {
		service.Spec.AppSelectorExpression = `app.metadata.namespace == "test-ns"`
		result, err := Matches(app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("propagates errors from evaluateExpression", func() {
		service.Spec.AppSelectorExpression = `app.metadata.namespace ==` // Invalid syntax
		result, err := Matches(app, service, fakeClient)

		Expect(err).To(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("validates default expression is correct", func() {
		// Verify DefaultExpression constant is valid
		service.Spec.AppSelectorExpression = DefaultExpression
		result, err := Matches(app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue()) // Same namespace
	})

	It("returns false for different namespaces with default expression", func() {
		app.Namespace = "other-ns"
		service.Spec.AppSelectorExpression = ""

		// Need to add the other namespace
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(
				&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "test-ns"}},
				&corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: "other-ns"}},
			).
			Build()

		result, err := Matches(app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse()) // Different namespaces
	})
})

var _ = Describe("ValidateExpression", func() {
	It("accepts valid expressions", func() {
		err := ValidateExpression(`app.metadata.namespace == "test"`)
		Expect(err).NotTo(HaveOccurred())
	})

	It("accepts empty expression", func() {
		err := ValidateExpression("")
		Expect(err).NotTo(HaveOccurred())
	})

	It("accepts complex valid expressions", func() {
		err := ValidateExpression(`app.metadata.namespace.startsWith("team-") && app.metadata.namespace.endsWith("-prod")`)
		Expect(err).NotTo(HaveOccurred())
	})

	It("rejects expression with syntax error", func() {
		err := ValidateExpression(`app.metadata.namespace ==`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to compile CEL expression"))
	})

	It("rejects expression returning string", func() {
		err := ValidateExpression(`app.metadata.namespace`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must return boolean"))
	})

	It("rejects expression returning int", func() {
		err := ValidateExpression(`app.spec.acceptor.port`)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must return boolean"))
	})

	It("rejects expression with type error", func() {
		err := ValidateExpression(`app.metadata.namespace + 123`)
		Expect(err).To(HaveOccurred())
	})
})

// Note: Invalid field references like `app.nonexistent.field` cannot be caught at
// compile time because the types are dynamic (maps). These errors only appear at
// evaluation time, which is tested in the "Invalid CEL Expressions" suite above.

var _ = Describe("Namespace Fetch Optimization", func() {
	It("skips fetching namespaces when expression doesn't reference them", func() {
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "myapp",
				Namespace: "team-a",
			},
		}
		service := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "shared-broker",
				Namespace: "shared",
			},
		}

		// Create a fake client WITHOUT any namespace objects
		// This simulates a scenario where namespace access is unavailable
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Expression that only uses app/service metadata (not namespace objects)
		// This should succeed even though namespaces don't exist
		expr := `app.metadata.namespace == service.metadata.namespace`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse()) // Different namespaces
	})

	It("fetches appNamespace only when expression references it", func() {
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "myapp",
				Namespace: "team-a",
			},
		}
		service := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "shared-broker",
				Namespace: "shared",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		// Create only the app namespace
		appNs := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name:   "team-a",
				Labels: map[string]string{"tier": "premium"},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(appNs).
			Build()

		// Expression uses appNamespace but not serviceNamespace
		expr := `has(appNamespace.metadata.labels) && appNamespace.metadata.labels["tier"] == "premium"`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("fetches serviceNamespace only when expression references it", func() {
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "myapp",
				Namespace: "team-a",
			},
		}
		service := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "shared-broker",
				Namespace: "shared",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		// Create only the service namespace
		svcNs := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name:   "shared",
				Labels: map[string]string{"tier": "shared"},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(svcNs).
			Build()

		// Expression uses serviceNamespace but not appNamespace
		expr := `has(serviceNamespace.metadata.labels) && serviceNamespace.metadata.labels["tier"] == "shared"`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("fetches both namespaces when expression references both", func() {
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "myapp",
				Namespace: "team-a",
			},
		}
		service := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "shared-broker",
				Namespace: "shared",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		// Create both namespaces
		appNs := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name:   "team-a",
				Labels: map[string]string{"env": "prod"},
			},
		}
		svcNs := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name:   "shared",
				Labels: map[string]string{"tier": "shared"},
			},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRuntimeObjects(appNs, svcNs).
			Build()

		// Expression uses both namespaces
		expr := `has(appNamespace.metadata.labels) && has(serviceNamespace.metadata.labels) && appNamespace.metadata.labels["env"] == "prod"`
		result, err := evaluateExpression(expr, app, service, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("caches variable usage analysis across evaluations", func() {
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "myapp",
				Namespace: "team-a",
			},
		}
		service := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      "shared-broker",
				Namespace: "shared",
			},
		}

		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(v1beta2.AddToScheme(scheme)).To(Succeed())

		fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

		// Same expression evaluated twice should use cached metadata
		expr := `app.metadata.namespace == "team-a"`

		result1, err1 := evaluateExpression(expr, app, service, fakeClient)
		Expect(err1).NotTo(HaveOccurred())
		Expect(result1).To(BeTrue())

		result2, err2 := evaluateExpression(expr, app, service, fakeClient)
		Expect(err2).NotTo(HaveOccurred())
		Expect(result2).To(BeTrue())

		// Both evaluations should succeed without namespace objects
		// This demonstrates the optimization is working
	})
})
