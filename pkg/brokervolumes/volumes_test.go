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

	It("returns a PVC volume when persistent", func() {
		vols := MakeVolumes("my-broker", true, nil, nil)
		Expect(vols).To(HaveLen(1))
		Expect(vols[0].PersistentVolumeClaim).NotTo(BeNil())
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
})

var _ = Describe("MakeVolumeMounts", func() {
	It("includes the persistent data mount and extra mounts", func() {
		extra := corev1.Volume{Name: "extra-vol"}
		mounts := MakeVolumeMounts("my-broker", []corev1.Volume{extra}, nil, nil)
		names := make([]string, len(mounts))
		for i, m := range mounts {
			names[i] = m.Name
		}
		Expect(names).To(ContainElements("my-broker", "extra-vol"))
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
})
