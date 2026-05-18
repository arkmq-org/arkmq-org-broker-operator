package pods

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var NameBuilder namer.NamerData

func MakePodTemplateSpec(current *corev1.PodTemplateSpec, namespacedName types.NamespacedName, labels map[string]string, annotations map[string]string) *corev1.PodTemplateSpec {

	var desired *corev1.PodTemplateSpec = current
	if desired == nil {
		desired = &corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespacedName.Name,
				Namespace: namespacedName.Namespace,
			},
			Spec: corev1.PodSpec{},
		}
	}
	desired.ObjectMeta.Labels = labels

	common.ApplyAnnotations(&desired.ObjectMeta, annotations)

	return desired
}
