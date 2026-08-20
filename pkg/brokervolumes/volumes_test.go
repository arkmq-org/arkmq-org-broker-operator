package brokervolumes

import (
	"context"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("MakeVolumes", func() {
	It("returns empty when not persistent and no extras", func() {
		Expect(MakeVolumes("my-broker", false, nil, nil)).To(BeEmpty())
	})

	It("returns a PVC volume when persistent with ClaimName matching the name argument", func() {
		vols := MakeVolumes("my-broker", true, nil, nil)
		Expect(vols).To(HaveLen(1))
		Expect(vols[0].PersistentVolumeClaim).NotTo(BeNil())
		Expect(vols[0].PersistentVolumeClaim.ClaimName).To(Equal("my-broker"))
	})

	It("mirrors the broker call site: persistent=true, one extra volume, one EPVC — asserts claim names and ordering", func() {
		// Matches: brokervolumes.MakeVolumes(customResource.Name, customResource.Spec.PersistenceEnabled,
		//   customResource.Spec.ExtraVolumes, customResource.Spec.ExtraVolumeClaimTemplates)
		extra := corev1.Volume{
			Name: "cfg-vol",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "broker-config"},
				},
			},
		}
		epvc := v1beta2.VolumeClaimTemplate{ObjectMeta: v1beta2.ObjectMeta{Name: "data-pvc"}}
		vols := MakeVolumes("my-broker", true, []corev1.Volume{extra}, []v1beta2.VolumeClaimTemplate{epvc})
		Expect(vols).To(HaveLen(3))
		// index 0: broker's own PVC — ClaimName must match the CR name
		Expect(vols[0].PersistentVolumeClaim).NotTo(BeNil())
		Expect(vols[0].PersistentVolumeClaim.ClaimName).To(Equal("my-broker"))
		// index 1: extra volume — identity preserved
		Expect(vols[1].Name).To(Equal("cfg-vol"))
		Expect(vols[1].ConfigMap).NotTo(BeNil())
		Expect(vols[1].ConfigMap.Name).To(Equal("broker-config"))
		// index 2: EPVC volume — ClaimName must match the EPVC name
		Expect(vols[2].PersistentVolumeClaim).NotTo(BeNil())
		Expect(vols[2].PersistentVolumeClaim.ClaimName).To(Equal("data-pvc"))
	})

	It("appends extra volumes", func() {
		extra := corev1.Volume{Name: "extra-vol"}
		vols := MakeVolumes("my-broker", false, []corev1.Volume{extra}, nil)
		Expect(vols).To(HaveLen(1))
		Expect(vols[0].Name).To(Equal("extra-vol"))
	})

	It("appends extra volume claim templates", func() {
		epvc := v1beta2.VolumeClaimTemplate{ObjectMeta: v1beta2.ObjectMeta{Name: "my-pvc"}}
		vols := MakeVolumes("my-broker", false, nil, []v1beta2.VolumeClaimTemplate{epvc})
		Expect(vols).To(HaveLen(1))
		Expect(vols[0].PersistentVolumeClaim.ClaimName).To(Equal("my-pvc"))
	})
})

var _ = Describe("MakeExtraVolumeMounts", func() {
	It("uses a default mount path when none is specified", func() {
		extra := corev1.Volume{
			Name: "extra-vol",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
				},
			},
		}
		mounts := MakeExtraVolumeMounts([]corev1.Volume{extra}, nil, nil)
		Expect(mounts).To(HaveLen(1))
		Expect(mounts[0].Name).To(Equal("extra-vol"))
		Expect(mounts[0].MountPath).NotTo(BeEmpty())
	})

	It("uses a custom mount path when specified", func() {
		extra := corev1.Volume{Name: "extra-vol"}
		customMount := corev1.VolumeMount{Name: "extra-vol", MountPath: "/custom/path"}
		mounts := MakeExtraVolumeMounts([]corev1.Volume{extra}, []corev1.VolumeMount{customMount}, nil)
		Expect(mounts).To(HaveLen(1))
		Expect(mounts[0].MountPath).To(Equal("/custom/path"))
	})

	It("falls back to default when mount path is empty", func() {
		extra := corev1.Volume{
			Name: "extra-vol",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "my-secret"},
			},
		}
		emptyMount := corev1.VolumeMount{Name: "extra-vol", MountPath: ""}
		mounts := MakeExtraVolumeMounts([]corev1.Volume{extra}, []corev1.VolumeMount{emptyMount}, nil)
		Expect(mounts).To(HaveLen(1))
		Expect(mounts[0].MountPath).NotTo(BeEmpty())
	})

	It("uses default EPVC mount path when no user mount is provided", func() {
		epvc := v1beta2.VolumeClaimTemplate{ObjectMeta: v1beta2.ObjectMeta{Name: "data-pvc"}}
		mounts := MakeExtraVolumeMounts(nil, nil, []v1beta2.VolumeClaimTemplate{epvc})
		Expect(mounts).To(HaveLen(1))
		Expect(mounts[0].Name).To(Equal("data-pvc"))
		Expect(mounts[0].MountPath).To(Equal("/opt/data-pvc/data"))
	})

	It("uses user-provided mount for EPVC when names match", func() {
		epvc := v1beta2.VolumeClaimTemplate{ObjectMeta: v1beta2.ObjectMeta{Name: "data-pvc"}}
		userMount := corev1.VolumeMount{
			Name:      "data-pvc",
			MountPath: "/custom/data",
			ReadOnly:  true,
			SubPath:   "subdir",
		}
		mounts := MakeExtraVolumeMounts(nil, []corev1.VolumeMount{userMount}, []v1beta2.VolumeClaimTemplate{epvc})
		Expect(mounts).To(HaveLen(1))
		Expect(mounts[0].MountPath).To(Equal("/custom/data"))
		Expect(mounts[0].ReadOnly).To(BeTrue())
		Expect(mounts[0].SubPath).To(Equal("subdir"))
	})

	It("mirrors the reconciler call site: extra volume with mount override and EPVC — asserts paths, ReadOnly, SubPath, and ordering", func() {
		// Matches: brokervolumes.MakeExtraVolumeMounts(
		//   customResource.Spec.ExtraVolumes,
		//   customResource.Spec.ExtraVolumeMounts,
		//   customResource.Spec.ExtraVolumeClaimTemplates)
		extra := corev1.Volume{
			Name: "cfg-vol",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "broker-config"},
				},
			},
		}
		extraMount := corev1.VolumeMount{
			Name:      "cfg-vol",
			MountPath: "/etc/broker/config",
			ReadOnly:  true,
			SubPath:   "broker.xml",
		}
		epvc := v1beta2.VolumeClaimTemplate{ObjectMeta: v1beta2.ObjectMeta{Name: "data-pvc"}}
		mounts := MakeExtraVolumeMounts([]corev1.Volume{extra}, []corev1.VolumeMount{extraMount}, []v1beta2.VolumeClaimTemplate{epvc})
		Expect(mounts).To(HaveLen(2))
		// index 0: extra volume — user override fully preserved
		Expect(mounts[0].Name).To(Equal("cfg-vol"))
		Expect(mounts[0].MountPath).To(Equal("/etc/broker/config"))
		Expect(mounts[0].ReadOnly).To(BeTrue())
		Expect(mounts[0].SubPath).To(Equal("broker.xml"))
		// index 1: EPVC — default mount path derived from EPVC name
		Expect(mounts[1].Name).To(Equal("data-pvc"))
		Expect(mounts[1].MountPath).To(Equal("/opt/data-pvc/data"))
	})
})

var _ = Describe("MakeVolumeMounts", func() {
	It("mirrors the broker call site: persistent mount at /app first, then extra mount with overrides, then EPVC mount", func() {
		// Matches: brokervolumes.MakeVolumeMounts(customResource.Name,
		//   customResource.Spec.ExtraVolumes, customResource.Spec.ExtraVolumeMounts,
		//   customResource.Spec.ExtraVolumeClaimTemplates)
		extra := corev1.Volume{
			Name: "cfg-vol",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "broker-config"},
				},
			},
		}
		extraMount := corev1.VolumeMount{
			Name:      "cfg-vol",
			MountPath: "/etc/broker/config",
			ReadOnly:  true,
			SubPath:   "broker.xml",
		}
		epvc := v1beta2.VolumeClaimTemplate{ObjectMeta: v1beta2.ObjectMeta{Name: "data-pvc"}}
		mounts := MakeVolumeMounts("my-broker", []corev1.Volume{extra}, []corev1.VolumeMount{extraMount}, []v1beta2.VolumeClaimTemplate{epvc})
		Expect(mounts).To(HaveLen(3))
		// index 0: persistent data mount — name is the CR name, path is the package-level DataMountPath constant
		Expect(mounts[0].Name).To(Equal("my-broker"))
		Expect(mounts[0].MountPath).To(Equal(DataMountPath))
		Expect(mounts[0].ReadOnly).To(BeFalse())
		// index 1: extra volume — user override fully preserved
		Expect(mounts[1].Name).To(Equal("cfg-vol"))
		Expect(mounts[1].MountPath).To(Equal("/etc/broker/config"))
		Expect(mounts[1].ReadOnly).To(BeTrue())
		Expect(mounts[1].SubPath).To(Equal("broker.xml"))
		// index 2: EPVC — default mount path derived from EPVC name
		Expect(mounts[2].Name).To(Equal("data-pvc"))
		Expect(mounts[2].MountPath).To(Equal("/opt/data-pvc/data"))
	})
})

var _ = Describe("RemovePVCOwnerRef", func() {
	var scheme *runtime.Scheme

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
	})

	It("removes the matching owner reference", func() {
		uid := types.UID("owner-uid")
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-broker-ss-0",
				Namespace: "test",
				OwnerReferences: []metav1.OwnerReference{
					{UID: "other-uid"},
					{UID: uid},
				},
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()
		pvcKey := types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}

		RemovePVCOwnerRef(pvcKey, uid, cl, ctrl.Log)

		updated := &corev1.PersistentVolumeClaim{}
		Expect(cl.Get(context.TODO(), pvcKey, updated)).To(Succeed())
		Expect(updated.OwnerReferences).To(HaveLen(1))
		Expect(updated.OwnerReferences[0].UID).To(Equal(types.UID("other-uid")))
	})

	It("is a no-op when the uid is not present", func() {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-broker-ss-0",
				Namespace: "test",
				OwnerReferences: []metav1.OwnerReference{
					{UID: "other-uid"},
				},
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()
		pvcKey := types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}

		RemovePVCOwnerRef(pvcKey, "non-existent-uid", cl, ctrl.Log)

		updated := &corev1.PersistentVolumeClaim{}
		Expect(cl.Get(context.TODO(), pvcKey, updated)).To(Succeed())
		Expect(updated.OwnerReferences).To(HaveLen(1))
	})

	It("is a no-op when the PVC does not exist", func() {
		// Empty store — Get returns NotFound. The function must return silently
		// without calling Update (which would panic on an unknown object).
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		pvcKey := types.NamespacedName{Name: "missing-pvc", Namespace: "test"}

		Expect(func() {
			RemovePVCOwnerRef(pvcKey, "any-uid", cl, ctrl.Log)
		}).NotTo(Panic())

		// Confirm the PVC was never created as a side-effect.
		ghost := &corev1.PersistentVolumeClaim{}
		Expect(cl.Get(context.TODO(), pvcKey, ghost)).NotTo(Succeed())
	})

	It("is a no-op when the PVC has no owner references", func() {
		// Exercises the early-return guard at line 95 of volumes.go.
		// Update must never be called, so the PVC remains unmodified.
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-broker-ss-0",
				Namespace: "test",
				// OwnerReferences intentionally absent
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()
		pvcKey := types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}

		RemovePVCOwnerRef(pvcKey, "any-uid", cl, ctrl.Log)

		updated := &corev1.PersistentVolumeClaim{}
		Expect(cl.Get(context.TODO(), pvcKey, updated)).To(Succeed())
		Expect(updated.OwnerReferences).To(BeEmpty())
	})

	It("removes all entries with the matching UID when the same UID appears more than once", func() {
		// The loop at lines 101-106 of volumes.go iterates every entry and
		// excludes all whose UID matches — not just the first occurrence.
		uid := types.UID("owner-uid")
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-broker-ss-0",
				Namespace: "test",
				OwnerReferences: []metav1.OwnerReference{
					{UID: uid, Name: "owner-a"},
					{UID: "other-uid", Name: "keeper"},
					{UID: uid, Name: "owner-b"},
				},
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()
		pvcKey := types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}

		RemovePVCOwnerRef(pvcKey, uid, cl, ctrl.Log)

		updated := &corev1.PersistentVolumeClaim{}
		Expect(cl.Get(context.TODO(), pvcKey, updated)).To(Succeed())
		Expect(updated.OwnerReferences).To(HaveLen(1))
		Expect(updated.OwnerReferences[0].UID).To(Equal(types.UID("other-uid")))
		Expect(updated.OwnerReferences[0].Name).To(Equal("keeper"))
	})
})
