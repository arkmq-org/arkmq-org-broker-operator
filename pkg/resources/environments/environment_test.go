package environments

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Environment", func() {

	Describe("HasExtraBrokerProperties", func() {
		It("returns false for nil", func() {
			Expect(HasExtraBrokerProperties(nil)).To(BeFalse())
		})

		It("returns false when the var is absent", func() {
			Expect(HasExtraBrokerProperties([]corev1.EnvVar{
				{Name: "OTHER_VAR", Value: "val"},
			})).To(BeFalse())
		})

		It("returns true when the var is present with a plain value", func() {
			Expect(HasExtraBrokerProperties([]corev1.EnvVar{
				{Name: ExtraBrokerPropertiesEnvVar, Value: "/path/to/props"},
			})).To(BeTrue())
		})

		It("returns true when the var is present with a valueFrom source", func() {
			Expect(HasExtraBrokerProperties([]corev1.EnvVar{
				{
					Name: ExtraBrokerPropertiesEnvVar,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "secret"},
							Key:                  "key",
						},
					},
				},
			})).To(BeTrue())
		})
	})

	Describe("AppendExtraBrokerPropertiesToken", func() {
		const baseVal = "-Dbroker.properties=/amq/extra/secrets/broker-props/broker.properties"

		Context("when EXTRA_BROKER_PROPERTIES is not set", func() {
			It("returns the value unchanged for nil envs", func() {
				Expect(AppendExtraBrokerPropertiesToken(baseVal, nil)).To(Equal(baseVal))
			})

			It("returns the value unchanged when the var is absent", func() {
				Expect(AppendExtraBrokerPropertiesToken(baseVal, []corev1.EnvVar{
					{Name: "OTHER_VAR", Value: "val"},
				})).To(Equal(baseVal))
			})
		})

		Context("when EXTRA_BROKER_PROPERTIES is set", func() {
			It("appends the token suffix", func() {
				Expect(AppendExtraBrokerPropertiesToken(baseVal, []corev1.EnvVar{
					{Name: ExtraBrokerPropertiesEnvVar, Value: "/path/to/props"},
				})).To(Equal(baseVal + ",$(EXTRA_BROKER_PROPERTIES)"))
			})
		})
	})
})
