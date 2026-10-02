package selectors

import (
	"k8s.io/apimachinery/pkg/labels"
)

const (
	LabelAppKey             = "application"
	LabelActiveMQArtemisKey = "ActiveMQArtemis"
	LabelBrokerKey          = "broker"
	// LabelPartOfKey is the Kubernetes recommended ownership label used so the
	// operator can limit its Pod watch/informer cache to broker-managed pods.
	LabelPartOfKey = "app.kubernetes.io/part-of"
	// LabelPartOfValue is the shared value for all pods managed by this operator.
	LabelPartOfValue = "broker.arkmq.org"

	// Standard Kubernetes label keys
	LabelAppKubernetesInstance  = "app.kubernetes.io/instance"
	LabelAppKubernetesComponent = "app.kubernetes.io/component"
	LabelAppKubernetesManagedBy = "app.kubernetes.io/managed-by"

	// Domain-specific label keys
	LabelBrokerService   = "broker.arkmq.org/service"
	LabelBrokerPeerIndex = "broker.arkmq.org/peer-index"

	// Standard Kubernetes annotation keys
	AnnotationStatefulSet        = "statefulsets.kubernetes.io/drainer-pod-owner"
	AnnotationDrainerPodTemplate = "statefulsets.kubernetes.io/drainer-pod-template"
	PodNameLabelKey              = "statefulset.kubernetes.io/pod-name"
)

type LabelerInterface interface {
	Labels() map[string]string
	Base(baseName string) *LabelerData
	Suffix(labelSuffix string) *LabelerData
	Generate()
}

type LabelerData struct {
	baseName    string
	suffix      string
	resourceKey string
	labels      map[string]string
}

func NewBrokerLabeler() *LabelerData {
	return &LabelerData{resourceKey: LabelBrokerKey}
}

func NewActiveMQArtemisLabeler() *LabelerData {
	return &LabelerData{resourceKey: LabelActiveMQArtemisKey}
}

func (l *LabelerData) Labels() map[string]string {
	return l.labels
}

func (l *LabelerData) Base(name string) *LabelerData {
	l.baseName = name
	return l
}

func (l *LabelerData) Suffix(labelSuffix string) *LabelerData {
	l.suffix = labelSuffix
	return l
}

func (l *LabelerData) Generate() {
	l.labels = make(map[string]string)
	l.labels[LabelAppKey] = l.baseName + "-" + l.suffix //"-app"
	l.labels[l.resourceKey] = l.baseName
	// Common ownership label on all operator-managed resources from pod-creating CRs.
	l.labels[LabelPartOfKey] = LabelPartOfValue
}

// OperatorPodLabelSelector returns the label selector used to limit the
// controller-runtime Pod watch cache to operator-managed broker pods.
func OperatorPodLabelSelector() labels.Selector {
	return labels.SelectorFromSet(labels.Set{
		LabelPartOfKey: LabelPartOfValue,
	})
}

func GetLabels(crName string) map[string]string {
	labelBuilder := NewActiveMQArtemisLabeler()
	labelBuilder.Base(crName).Suffix("app").Generate()
	return labelBuilder.Labels()
}
