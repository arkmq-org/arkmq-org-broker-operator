package controllers

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	routev1 "github.com/openshift/api/route/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	pointer "k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func extractSha(status brokerStatus, name string) (string, error) {
	current, present := status.BrokerConfigStatus.PropertiesStatus[name]
	if !present {
		return "", errors.New("not present")
	} else {
		return current.Alder32, nil
	}
}

func testFormattedObject(formattedObject map[string]interface{}, expectedString string) {
	Expect(formattedObject["plain-string"]).To(Equal("TEST"))
	Expect(formattedObject["plain-int"]).To(Equal(1))
	Expect(formattedObject["test-map"].(map[string]interface{})["test-map-key"]).To(Equal(expectedString))
	Expect(formattedObject["test-map"].(map[string]interface{})["nested-plain-string"]).To(Equal("TEST"))
	Expect(formattedObject["test-map"].(map[string]interface{})["nested-plain-int"]).To(Equal(1))
	Expect(formattedObject["test-map"].(map[string]interface{})["nested-test-array"].([]interface{})[0]).To(Equal(expectedString))
	Expect(formattedObject["test-array"].([]interface{})[0]).To(Equal(expectedString))
	Expect(formattedObject["test-array"].([]interface{})[1].(map[string]interface{})["nested-test-map-key"]).To(Equal(expectedString))
	Expect(formattedObject["test-string"]).To(Equal(expectedString))
}

var _ = Describe("brokercluster reconciler", func() {

	It("hex sha hash of map", Label(unitLabel), func() {
		nilOne := hexShaHashOfMap(nil)
		nilTwo := hexShaHashOfMap(nil)

		Expect(nilOne).To(Equal(nilTwo))

		props := []string{"a=a", "b=b"}

		propsOriginal := hexShaHashOfMap(props)

		props = append(props, "c=c")

		propsModified := hexShaHashOfMap(props)

		Expect(propsOriginal).NotTo(Equal(propsModified))

		props = props[:2]

		Expect(hexShaHashOfMap(props)).To(Equal(propsOriginal))

		props = props[:1]

		Expect(hexShaHashOfMap(props)).NotTo(Equal(propsOriginal))
	})

	It("map comparator for stateful set", Label(unitLabel), func() {
		ss := &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1beta1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:                       "ss",
				GenerateName:               "",
				Namespace:                  "a",
				SelfLink:                   "",
				UID:                        "",
				ResourceVersion:            "1",
				Generation:                 0,
				CreationTimestamp:          metav1.Time{},
				DeletionTimestamp:          &metav1.Time{},
				DeletionGracePeriodSeconds: new(int64),
				Labels:                     nil,
				Annotations:                nil,
				OwnerReferences:            []metav1.OwnerReference{},
				Finalizers:                 []string{},
				ManagedFields:              []metav1.ManagedFieldsEntry{},
			},
			Spec:   appsv1.StatefulSetSpec{},
			Status: appsv1.StatefulSetStatus{},
		}

		ssMod := &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:                       "ss",
				GenerateName:               "",
				Namespace:                  "a",
				SelfLink:                   "",
				UID:                        "",
				ResourceVersion:            "1",
				Generation:                 0,
				CreationTimestamp:          metav1.Time{},
				DeletionTimestamp:          &metav1.Time{},
				DeletionGracePeriodSeconds: new(int64),
				Labels:                     nil,
				Annotations:                nil,
				OwnerReferences:            []metav1.OwnerReference{},
				Finalizers:                 []string{},
				ManagedFields:              []metav1.ManagedFieldsEntry{},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas:             new(int32),
				Selector:             &metav1.LabelSelector{},
				Template:             v1.PodTemplateSpec{},
				VolumeClaimTemplates: []v1.PersistentVolumeClaim{},
				ServiceName:          "ssMod",
				PodManagementPolicy:  "",
				UpdateStrategy:       appsv1.StatefulSetUpdateStrategy{},
				RevisionHistoryLimit: new(int32),
				MinReadySeconds:      0,
			},
			Status: appsv1.StatefulSetStatus{},
		}

		ss0 := &appsv1.StatefulSet{
			TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:                       "ss0",
				GenerateName:               "",
				Namespace:                  "a",
				SelfLink:                   "",
				UID:                        "",
				ResourceVersion:            "1",
				Generation:                 0,
				CreationTimestamp:          metav1.Time{},
				DeletionTimestamp:          &metav1.Time{},
				DeletionGracePeriodSeconds: new(int64),
				Labels:                     nil,
				Annotations:                nil,
				OwnerReferences:            []metav1.OwnerReference{},
				Finalizers:                 []string{},
				ManagedFields:              []metav1.ManagedFieldsEntry{},
			},
			Spec:   appsv1.StatefulSetSpec{},
			Status: appsv1.StatefulSetStatus{},
		}

		var requestedResources []client.Object

		requestedResources = append(requestedResources, ss0)

		requestedResources = append(requestedResources, ssMod)

		deployed := make(map[reflect.Type][]client.Object)
		var deployedSets []client.Object
		deployedSets = append(deployedSets, ss)

		ssType := reflect.ValueOf(ss).Elem().Type()
		deployed[ssType] = deployedSets

		requested := compare.NewMapBuilder().Add(requestedResources...).ResourceMap()
		comparator := compare.MapComparator{
			Comparator: compare.SimpleComparator(),
		}

		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: nil,
		}

		comparator.Comparator.SetComparator(reflect.TypeOf(appsv1.StatefulSet{}), reconciler.CompareMetaAndSpec)
		deltas := comparator.Compare(deployed, requested)

		Expect(deltas[ssType].Added).To(HaveLen(1))
		Expect(deltas[ssType].Updated).To(HaveLen(1))
	})

	It("comparator meta and spec", Label(unitLabel), func() {
		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: nil,
		}

		ss0 := &appsv1.StatefulSet{}
		equal := reconciler.CompareMetaAndSpec(ss0, ss0)

		Expect(equal).To(BeTrue())

		ss1 := &appsv1.StatefulSet{}
		ss1.Annotations = map[string]string{"A": "B"}
		equal = reconciler.CompareMetaAndSpec(ss0, ss1)

		Expect(equal).To(BeFalse())
	})

	It("get single stateful set status", Label(unitLabel), func() {
		expected := int32(1)
		ss := &appsv1.StatefulSet{}
		ss.Name = "joe"
		ss.Spec.Replicas = &expected
		ss.Status.Replicas = 1
		ss.Status.ReadyReplicas = 1

		cr := &v1beta2.BrokerCluster{}
		statusRunning := common.GetSingleStatefulSetStatus(ss, cr)
		Expect(statusRunning.Ready[0]).To(Equal("joe-0"))

		ss.Status.Replicas = 0
		ss.Status.ReadyReplicas = 0

		statusRunning = common.GetSingleStatefulSetStatus(ss, cr)
		Expect(statusRunning.Stopped[0]).To(Equal("joe"))

		expectedTwo := int32(2)
		ss.Spec.Replicas = &expectedTwo
		ss.Status.Replicas = 2
		ss.Status.ReadyReplicas = 1

		statusRunning = common.GetSingleStatefulSetStatus(ss, cr)
		Expect(statusRunning.Ready[0]).To(Equal("joe-0"))
		Expect(statusRunning.Starting[0]).To(Equal("joe-1"))
		Expect(cr.Status.DeploymentPlanSize).To(Equal(int32(2)))
	})

	It("get config applied config map name", Label(unitLabel), func() {
		cr := v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test",
			},
		}
		name := getPropertiesResourceNsName(&cr)
		Expect(name.Namespace).To(Equal("test-ns"))
		Expect(name.Name).To(Equal("test-props"))
	})

	It("extract sha", Label(unitLabel), func() {
		json := `{"configuration": {"properties": {"a_status.properties": {"alder32": "123456"}}}}`
		status, err := unmarshallStatus(json)
		Expect(err).NotTo(HaveOccurred())
		sha, err := extractSha(status, "a_status.properties")
		Expect(sha).To(Equal("123456"))
		Expect(err).NotTo(HaveOccurred())

		json = `{"configuration": {"properties": {"a_status.properties": {}}}}`
		status, err = unmarshallStatus(json)
		Expect(err).NotTo(HaveOccurred())
		sha, err = extractSha(status, "a_status.properties")
		Expect(sha).To(BeEmpty())
		Expect(err).NotTo(HaveOccurred())

		json = `you shall fail`
		status, err = unmarshallStatus(json)
		Expect(err).To(HaveOccurred())
		sha, err = extractSha(status, "a_status.properties")
		Expect(sha).To(BeEmpty())
		Expect(err).To(HaveOccurred())
	})

	It("extract errors", Label(unitLabel), func() {
		json := "{\"configuration\":{\"properties\":{\"broker.properties\":{\"alder32\":\"1\"},\"system\":{\"alder32\":\"1\"}}},\"server\":{\"jaas\":{\"properties\":{\"artemis-users.properties\":{\"reloadTime\":\"1669744377685\",\"Alder32\":\"955331033\"},\"artemis-roles.properties\":{\"reloadTime\":\"1669744377685\",\"Alder32\":\"701302135\"}}},\"state\":\"STARTED\",\"version\":\"2.27.0\",\"nodeId\":\"a644c0c6-700e-11ed-9d4f-0a580ad90188\",\"identity\":null,\"uptime\":\"33.176 seconds\"}}"
		status, err := unmarshallStatus(json)
		Expect(err).NotTo(HaveOccurred())
		sha, err := extractSha(status, "broker.properties")
		Expect(sha).To(Equal("1"))
		Expect(err).NotTo(HaveOccurred())

		json = `{"configuration": {
				"properties": {
					"a_status.properties": {
						"alder32": "110827957",
						"cr:alder32": "1f4004ae",
						"errors": []
					},
					"broker.properties": {
						"alder32": "524289198",
						"errors": [
							{
								"value": "notValid=bla",
								"reason": "No accessor method descriptor for: notValid on: class org.apache.activemq.artemis.core.config.impl.FileConfiguration"
							}
						]
					}
				}
			}
		}`
		status, err = unmarshallStatus(json)
		Expect(err).NotTo(HaveOccurred())
		appplyErrors := status.BrokerConfigStatus.PropertiesStatus["broker.properties"].ApplyErrors
		Expect(len(appplyErrors) > 0).To(BeTrue())

		marshalledErrorsStr := marshallApplyErrors(appplyErrors)
		Expect(marshalledErrorsStr).To(ContainSubstring("bla"))
	})

	It("new pod template spec for CR includes debug args", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraMounts: v1beta2.ExtraMountsType{
						ConfigMaps: []string{
							"some-cm",
						},
						Secrets: []string{
							"test-config-jaas-config",
							"other",
						},
					},
				},
			},
		}

		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: cr,
		}

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())
		expectedEnv := v1.EnvVar{
			Name:  "DEBUG_ARGS",
			Value: "-Djava.security.auth.login.config=/amq/extra/secrets/test-config-jaas-config/login.config",
		}
		Expect(newSpec.Spec.Containers[0].Env).To(ContainElement(expectedEnv))
	})

	It("new pod template spec for CR includes bp config map path", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraMounts: v1beta2.ExtraMountsType{
						ConfigMaps: []string{
							"my-config-bp",
						},
					},
				},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)
		fakeClient := fake.NewClientBuilder().Build()

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())

		found := false
		for _, env := range newSpec.Spec.Containers[0].Env {
			if env.Name == jdkJavaOptionsEnvVarName {
				Expect(env.Value).To(ContainSubstring("/amq/extra/configmaps/my-config-bp/"))
				Expect(env.Value).To(ContainSubstring("/amq/extra/configmaps/my-config-bp/broker-${STATEFUL_SET_ORDINAL}/"))
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "expected JDK_JAVA_OPTIONS env var with configmap -bp path")
	})

	It("new pod template spec for CR includes bp config map and secret paths", Label(unitLabel), func() {
		bpSecret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-secret-bp",
				Namespace: "",
			},
			Data: map[string][]byte{
				"address.properties": []byte("addressConfigurations.test.routingTypes=ANYCAST\n"),
			},
		}

		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraMounts: v1beta2.ExtraMountsType{
						ConfigMaps: []string{
							"my-config-bp",
						},
						Secrets: []string{
							"my-secret-bp",
						},
					},
				},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)
		fakeClient := fake.NewClientBuilder().WithObjects(bpSecret).Build()

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())

		for _, env := range newSpec.Spec.Containers[0].Env {
			if env.Name == jdkJavaOptionsEnvVarName {
				Expect(env.Value).To(ContainSubstring("/amq/extra/configmaps/my-config-bp/"))
				Expect(env.Value).To(ContainSubstring("/amq/extra/secrets/my-secret-bp/"))
				break
			}
		}
	})

	It("new pod template spec for CR non bp config map not in broker properties", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraMounts: v1beta2.ExtraMountsType{
						ConfigMaps: []string{
							"my-plain-configmap",
						},
					},
				},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)
		fakeClient := fake.NewClientBuilder().Build()

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, fakeClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())

		for _, env := range newSpec.Spec.Containers[0].Env {
			if env.Name == jdkJavaOptionsEnvVarName {
				Expect(env.Value).NotTo(ContainSubstring("my-plain-configmap"))
				break
			}
		}
	})

	It("process template includes labels service and secret", Label(unitLabel), func() {
		kindMatch := "Secret"
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					Labels: map[string]string{"myPodKey": "myPodValue"},
				},
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Labels: map[string]string{"myKey": "myValue"},
					},
					{
						Selector: &v1beta2.ResourceSelector{
							Kind: &kindMatch,
						},
						Labels: map[string]string{"mySecretKey": "mySecretValue"},
					}},
			},
		}
		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("TestProcess_TemplateIncludesLabelsServiceAndSecret"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		newSS, err := reconciler.ProcessStatefulSet(cr, *namer, nil)
		reconciler.trackDesired(newSS)
		Expect(err).NotTo(HaveOccurred())

		reconciler.ProcessDeploymentPlan(cr, *namer, nil, nil, newSS)

		fakeClient := fake.NewClientBuilder().Build()
		err = reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).NotTo(HaveOccurred())

		var ssFound = false
		var secretFound = false
		var serviceFound = false
		for _, resource := range common.ToResourceList(reconciler.requestedResources) {
			if ss, ok := resource.(*appsv1.StatefulSet); ok {
				newSpec := ss.Spec.Template
				Expect(newSpec).NotTo(BeNil())

				v, ok := newSpec.Labels["myPodKey"]
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("myPodValue"))

				ssFound = true
			}

			if secret, ok := resource.(*v1.Secret); ok {
				Expect(len(secret.Labels) >= 1).To(BeTrue())
				Expect(secret.Labels["myKey"]).To(Equal("myValue"))
				Expect(secret.Labels["mySecretKey"]).To(Equal("mySecretValue"))
				secretFound = true
			}

			if service, ok := resource.(*v1.Service); ok {
				Expect(len(service.Labels) >= 1).To(BeTrue())
				Expect(service.Labels["myKey"]).To(Equal("myValue"))
				_, found := service.Labels["mySecretKey"]
				Expect(found).To(BeFalse())
				serviceFound = true
			}
		}
		Expect(ssFound).To(BeTrue())
		Expect(secretFound).To(BeTrue())
		Expect(serviceFound).To(BeTrue())
	})

	It("process template includes labels secret regexp", Label(unitLabel), func() {
		regexpNameMatch := ".*-props"
		exactNameMatch := "-props"

		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Selector: &v1beta2.ResourceSelector{
							Name: &regexpNameMatch,
						},
						Labels: map[string]string{"mySecretKey": "mySecretValue"},
					},
					{
						Selector: &v1beta2.ResourceSelector{
							Name: &exactNameMatch,
						},
						Labels: map[string]string{"myExactSecretKey": "myExactSecretValue"},
					}},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("TestProcess_TemplateIncludesLabelsServiceAndSecret"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
		reconciler.ProcessDeploymentPlan(cr, *namer, nil, nil, newSS)

		fakeClient := fake.NewClientBuilder().Build()
		err := reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).NotTo(HaveOccurred())

		var secretFound = false
		var serviceFound = false

		for _, resource := range common.ToResourceList(reconciler.requestedResources) {
			if secret, ok := resource.(*v1.Secret); ok {
				Expect(len(secret.Labels) >= 1).To(BeTrue())
				Expect(secret.Labels["mySecretKey"]).To(Equal("mySecretValue"))
				Expect(secret.Labels["myExactSecretKey"]).To(Equal("myExactSecretValue"))
				secretFound = true
			}

			if service, ok := resource.(*v1.Service); ok {
				Expect(len(service.Labels) >= 1).To(BeTrue())
				_, found := service.Labels["mySecretKey"]
				Expect(found).To(BeFalse())
				_, found = service.Labels["myExactSecretKey"]
				Expect(found).To(BeFalse())
				serviceFound = true
			}
		}
		Expect(secretFound).To(BeTrue())
		Expect(serviceFound).To(BeTrue())
	})

	It("process template duplicate key replaces ok", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Labels: map[string]string{"mySecretKey": "mySecretValueWillBeReplacedByDuplicate"},
					},
					{
						Labels: map[string]string{"mySecretKey": "mySecretValue"},
					}},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("TestProcess_TemplateDuplicateKeyReplacesOk"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
		reconciler.ProcessDeploymentPlan(cr, *namer, nil, nil, newSS)

		fakeClient := fake.NewClientBuilder().Build()
		err := reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).NotTo(HaveOccurred())

		var secretFound = false
		for _, resource := range common.ToResourceList(reconciler.requestedResources) {
			if secret, ok := resource.(*v1.Secret); ok {
				Expect(len(secret.Labels) >= 1).To(BeTrue())
				Expect(secret.Labels["mySecretKey"]).To(Equal("mySecretValue"))
				secretFound = true
			}
		}
		Expect(secretFound).To(BeTrue())
	})

	It("respect existing JAVA_OPTS properties def", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec:       v1beta2.BrokerClusterSpec{},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("Test_Respect_existing_JAVA_OPTS_properties_def"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		existingSS := appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{Name: namer.SsNameBuilder.Name()},
			Spec: appsv1.StatefulSetSpec{
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						InitContainers: []v1.Container{
							{
								Name: cr.Name + "-container-init",
								Env: []v1.EnvVar{
									{
										Name:  javaOptsEnvVarName,
										Value: "a",
									},
								},
							},
						},
					},
				},
			},
		}
		reconciler.deployed = make(map[reflect.Type][]client.Object)
		reconciler.addToDeployed(reflect.TypeOf(appsv1.StatefulSet{}), &existingSS)
		newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)

		var index = -1
		for i, env := range newSS.Spec.Template.Spec.InitContainers[0].Env {
			if env.Name == javaOptsEnvVarName {
				index = i
				break
			}
		}
		Expect(index != -1).To(BeTrue())
		Expect(strings.Contains(newSS.Spec.Template.Spec.InitContainers[0].Env[index].Value, "properties")).To(BeTrue())
	})

	It("extra broker properties absent when not set", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec:       v1beta2.BrokerClusterSpec{},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("Test_ExtraBrokerPropertiesAbsentWhenNotSet"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		newSS, err := reconciler.ProcessStatefulSet(cr, *MakeNamers(cr), nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(newSS).NotTo(BeNil())

		env := newSS.Spec.Template.Spec.Containers[0].Env

		for _, e := range env {
			Expect(e.Name).NotTo(Equal(environments.ExtraBrokerPropertiesEnvVar),
				"EXTRA_BROKER_PROPERTIES must not be injected when user has not set it")
			if e.Name == jdkJavaOptionsEnvVarName {
				Expect(strings.Contains(e.Value, "$(EXTRA_BROKER_PROPERTIES)")).To(BeFalse(),
					"JDK_JAVA_OPTIONS must not contain the token when EXTRA_BROKER_PROPERTIES is unset")
			}
		}
	})

	It("extra broker properties user value appears in env and token in jdk opts", Label(unitLabel), func() {
		const extraPaths = "/my/custom/path/,/my/other/path/"

		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec: v1beta2.BrokerClusterSpec{
				Env: []v1.EnvVar{
					{
						Name:  environments.ExtraBrokerPropertiesEnvVar,
						Value: extraPaths,
					},
				},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("Test_ExtraBrokerPropertiesUserValueAppearsInEnvAndTokenInJdkOpts"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		newSS, err := reconciler.ProcessStatefulSet(cr, *MakeNamers(cr), nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(newSS).NotTo(BeNil())

		env := newSS.Spec.Template.Spec.Containers[0].Env

		extraPathsIdx := -1
		jdkOptsIdx := -1
		for i, e := range env {
			if e.Name == environments.ExtraBrokerPropertiesEnvVar {
				Expect(e.Value).To(Equal(extraPaths))
				extraPathsIdx = i
			}
			if e.Name == jdkJavaOptionsEnvVarName {
				jdkOptsIdx = i
			}
		}
		Expect(extraPathsIdx != -1).To(BeTrue(), "EXTRA_BROKER_PROPERTIES must be in container env")
		Expect(jdkOptsIdx != -1).To(BeTrue(), "JDK_JAVA_OPTIONS must be in container env")
		Expect(extraPathsIdx < jdkOptsIdx).To(BeTrue(), "EXTRA_BROKER_PROPERTIES must precede JDK_JAVA_OPTIONS")
		Expect(strings.Contains(env[jdkOptsIdx].Value, ",$(EXTRA_BROKER_PROPERTIES)")).To(BeTrue(),
			"JDK_JAVA_OPTIONS must contain ,$(EXTRA_BROKER_PROPERTIES) token")
	})

	Context("extra broker properties value from passed through", func() {
		It("secretKeyRef", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cr"},
				Spec: v1beta2.BrokerClusterSpec{
					Env: []v1.EnvVar{
						{
							Name: environments.ExtraBrokerPropertiesEnvVar,
							ValueFrom: &v1.EnvVarSource{
								SecretKeyRef: &v1.SecretKeySelector{
									LocalObjectReference: v1.LocalObjectReference{Name: "my-secret"},
									Key:                  "props-path",
								},
							},
						},
					},
				},
			}

			outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("Test_ExtraBrokerPropertiesValueFromPassedThrough"), isOpenshift, false)
			reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

			newSS, err := reconciler.ProcessStatefulSet(cr, *MakeNamers(cr), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(newSS).NotTo(BeNil())

			env := newSS.Spec.Template.Spec.Containers[0].Env

			extraPathsIdx := -1
			jdkOptsIdx := -1
			for i, e := range env {
				if e.Name == environments.ExtraBrokerPropertiesEnvVar {
					Expect(e.ValueFrom).NotTo(BeNil())
					Expect(e.ValueFrom.SecretKeyRef).NotTo(BeNil())
					Expect(e.ValueFrom.SecretKeyRef.Name).To(Equal("my-secret"))
					Expect(e.ValueFrom.SecretKeyRef.Key).To(Equal("props-path"))
					extraPathsIdx = i
				}
				if e.Name == jdkJavaOptionsEnvVarName {
					jdkOptsIdx = i
				}
			}
			Expect(extraPathsIdx != -1).To(BeTrue(), "EXTRA_BROKER_PROPERTIES must be present in container env")
			Expect(jdkOptsIdx != -1).To(BeTrue(), "JDK_JAVA_OPTIONS must be present in container env")
			Expect(extraPathsIdx < jdkOptsIdx).To(BeTrue(), "EXTRA_BROKER_PROPERTIES must precede JDK_JAVA_OPTIONS in env list")
			Expect(strings.Contains(env[jdkOptsIdx].Value, ",$(EXTRA_BROKER_PROPERTIES)")).To(BeTrue(),
				"JDK_JAVA_OPTIONS must contain ,$(EXTRA_BROKER_PROPERTIES) token")
		})

		It("configMapKeyRef", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cr"},
				Spec: v1beta2.BrokerClusterSpec{
					Env: []v1.EnvVar{
						{
							Name: environments.ExtraBrokerPropertiesEnvVar,
							ValueFrom: &v1.EnvVarSource{
								ConfigMapKeyRef: &v1.ConfigMapKeySelector{
									LocalObjectReference: v1.LocalObjectReference{Name: "my-cm"},
									Key:                  "props-path",
								},
							},
						},
					},
				},
			}

			outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("Test_ExtraBrokerPropertiesValueFromPassedThrough"), isOpenshift, false)
			reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

			newSS, err := reconciler.ProcessStatefulSet(cr, *MakeNamers(cr), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(newSS).NotTo(BeNil())

			env := newSS.Spec.Template.Spec.Containers[0].Env

			extraPathsIdx := -1
			jdkOptsIdx := -1
			for i, e := range env {
				if e.Name == environments.ExtraBrokerPropertiesEnvVar {
					Expect(e.ValueFrom).NotTo(BeNil())
					Expect(e.ValueFrom.ConfigMapKeyRef).NotTo(BeNil())
					Expect(e.ValueFrom.ConfigMapKeyRef.Name).To(Equal("my-cm"))
					Expect(e.ValueFrom.ConfigMapKeyRef.Key).To(Equal("props-path"))
					extraPathsIdx = i
				}
				if e.Name == jdkJavaOptionsEnvVarName {
					jdkOptsIdx = i
				}
			}
			Expect(extraPathsIdx != -1).To(BeTrue(), "EXTRA_BROKER_PROPERTIES must be present in container env")
			Expect(jdkOptsIdx != -1).To(BeTrue(), "JDK_JAVA_OPTIONS must be present in container env")
			Expect(extraPathsIdx < jdkOptsIdx).To(BeTrue(), "EXTRA_BROKER_PROPERTIES must precede JDK_JAVA_OPTIONS in env list")
			Expect(strings.Contains(env[jdkOptsIdx].Value, ",$(EXTRA_BROKER_PROPERTIES)")).To(BeTrue(),
				"JDK_JAVA_OPTIONS must contain ,$(EXTRA_BROKER_PROPERTIES) token")
		})
	})

	It("process template key value", Label(unitLabel), func() {
		kindMatch := "Service"
		matchOrdinalServices := ".+-[0-9]+-svc"
		matchGvForIngress := "networking.k8s.io/v1"
		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec: v1beta2.BrokerClusterSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Labels: map[string]string{"myKey": "myValue-$(CR_NAME)"},
					},
					{
						Selector: &v1beta2.ResourceSelector{
							Kind: &kindMatch,
							Name: &matchOrdinalServices,
						},
						Labels: map[string]string{"myKey-$(CR_NAME)": "myValue-$(BROKER_ORDINAL)"},
					},
					{
						Selector: &v1beta2.ResourceSelector{
							APIGroup: &matchGvForIngress,
						},
						Annotations: map[string]string{"myIngressKey-$(CR_NAME)": "myValue-$(BROKER_ORDINAL)"},
					},
				},
				Acceptors: []v1beta2.AcceptorType{{
					Name:   "aa",
					Port:   563,
					Expose: true,
				}},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
		reconciler.trackDesired(newSS)

		err := routev1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		fakeClient := fake.NewClientBuilder().Build()
		err = reconciler.ProcessAcceptorsAndConnectors(cr, *namer,
			fakeClient, nil, newSS)
		Expect(err).NotTo(HaveOccurred())

		err = reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).NotTo(HaveOccurred())

		var secretFound = false
		var serviceFound = false
		var ssFound = false
		for _, resource := range common.ToResourceList(reconciler.requestedResources) {
			if ss, ok := resource.(*appsv1.StatefulSet); ok {
				v, ok := ss.Labels["myKey"]
				Expect(ok).To(BeTrue())
				Expect(v).To(Equal("myValue-cr"))
				ssFound = true
			}

			if secret, ok := resource.(*v1.Secret); ok {
				Expect(len(secret.Labels) >= 1).To(BeTrue())
				Expect(secret.Labels["myKey"]).To(Equal("myValue-cr"))
				secretFound = true
			}

			if service, ok := resource.(*v1.Service); ok {
				Expect(len(service.Labels) >= 1).To(BeTrue())
				Expect(service.Labels["myKey"]).To(Equal("myValue-cr"))

				if strings.Contains(service.GetName(), "-0-") {
					Expect(service.Labels["myKey-cr"]).To(Equal("myValue-0"))
					serviceFound = true
				}
			}

			if ingress, ok := resource.(*netv1.Ingress); ok {
				Expect(len(ingress.Annotations) >= 1).To(BeTrue())
				Expect(ingress.Annotations["myIngressKey-cr"]).To(Equal("myValue-0"))
			}
		}
		Expect(ssFound).To(BeTrue())
		Expect(secretFound).To(BeTrue())
		Expect(serviceFound).To(BeTrue())
	})

	It("process template custom attribute ingress", Label(unitLabel), func() {
		matchGvForIngress := "networking.k8s.io/v1"
		var ingressClassVal = "SomeClass"
		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec: v1beta2.BrokerClusterSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Selector: &v1beta2.ResourceSelector{
							APIGroup: &matchGvForIngress,
						},
						Annotations: map[string]string{"myIngressKey-$(CR_NAME)": "myValue-$(BROKER_ORDINAL)"},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"ingressClassName": ingressClassVal,
							},
						},
						}),
					},
				},
				Acceptors: []v1beta2.AcceptorType{{
					Name:       "aa",
					Port:       563,
					Expose:     true,
					SSLEnabled: false,
					ExposeMode: &v1beta2.ExposeModes.Ingress,
				}},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
		reconciler.trackDesired(newSS)

		fakeClient := fake.NewClientBuilder().Build()
		err := reconciler.ProcessAcceptorsAndConnectors(cr, *namer,
			fakeClient, nil, newSS)
		Expect(err).NotTo(HaveOccurred())

		err = reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).NotTo(HaveOccurred())

		var ingressOk = false
		for _, resource := range common.ToResourceList(reconciler.requestedResources) {
			if ingress, ok := resource.(*netv1.Ingress); ok {
				Expect(len(ingress.Annotations) >= 1).To(BeTrue())
				Expect(ingress.Annotations["myIngressKey-cr"]).To(Equal("myValue-0"))
				Expect(ingress.Spec.IngressClassName).NotTo(BeNil())
				Expect(*ingress.Spec.IngressClassName).To(Equal(ingressClassVal))
				ingressOk = true
			}
		}
		Expect(ingressOk).To(BeTrue())
	})

	It("process template custom attribute mis spelling ingress", Label(unitLabel), func() {
		matchGvForIngress := "networking.k8s.io/v1"
		var ingressClassVal = "SomeClass"
		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec: v1beta2.BrokerClusterSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Selector: &v1beta2.ResourceSelector{
							APIGroup: &matchGvForIngress,
						},
						Annotations: map[string]string{"myIngressKey-$(CR_NAME)": "myValue-$(BROKER_ORDINAL)"},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"ingressClazzName": ingressClassVal,
							},
						},
						}),
					},
				},
				Acceptors: []v1beta2.AcceptorType{{
					Name:       "aa",
					Port:       563,
					Expose:     true,
					SSLEnabled: false,
					ExposeMode: &v1beta2.ExposeModes.Ingress,
				}},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)
		newSS, err := reconciler.ProcessStatefulSet(cr, *namer, nil)
		Expect(err).NotTo(HaveOccurred())

		fakeClient := fake.NewClientBuilder().Build()
		err = reconciler.ProcessAcceptorsAndConnectors(cr, *namer,
			fakeClient, nil, newSS)
		Expect(err).NotTo(HaveOccurred())

		err = reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Clazz"))
	})

	Context("process template custom attribute container security context", func() {
		It("without CR name var", Label(unitLabel), func() {
			kindMatchSs := "StatefulSet"
			containerName := "cr-container"

			cr := &v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cr"},
				Spec: v1beta2.BrokerClusterSpec{
					ResourceTemplates: []v1beta2.ResourceTemplate{
						{
							Selector: &v1beta2.ResourceSelector{
								Kind: &kindMatchSs,
							},
							Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
								"spec": map[string]interface{}{
									"template": map[string]interface{}{
										"spec": map[string]interface{}{
											"containers": []interface{}{
												map[string]interface{}{
													"name": containerName,
													"securityContext": map[string]interface{}{
														"runAsNonRoot": true,
													},
												},
											},
										},
									},
								},
							},
							}),
						},
					},
				},
			}

			outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
			reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

			namer := MakeNamers(cr)

			newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
			reconciler.trackDesired(newSS)

			fakeClient := fake.NewClientBuilder().Build()
			err := reconciler.ProcessResources(cr, fakeClient, nil)
			Expect(err).NotTo(HaveOccurred())

			var runAsRootOk = false
			for _, resource := range common.ToResourceList(reconciler.requestedResources) {
				if ss, ok := resource.(*appsv1.StatefulSet); ok {
					Expect(ss.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).NotTo(BeNil())
					Expect(*ss.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(BeTrue())
					Expect(ss.Spec.Template.Spec.Containers[0].Image).NotTo(Equal(""))
					runAsRootOk = true
				}
			}
			Expect(runAsRootOk).To(BeTrue())
		})

		It("with CR name var", Label(unitLabel), func() {
			kindMatchSs := "StatefulSet"
			containerName := "$(CR_NAME)-container"

			cr := &v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cr"},
				Spec: v1beta2.BrokerClusterSpec{
					ResourceTemplates: []v1beta2.ResourceTemplate{
						{
							Selector: &v1beta2.ResourceSelector{
								Kind: &kindMatchSs,
							},
							Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
								"spec": map[string]interface{}{
									"template": map[string]interface{}{
										"spec": map[string]interface{}{
											"containers": []interface{}{
												map[string]interface{}{
													"name": containerName,
													"securityContext": map[string]interface{}{
														"runAsNonRoot": true,
													},
												},
											},
										},
									},
								},
							},
							}),
						},
					},
				},
			}

			outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
			reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

			namer := MakeNamers(cr)

			newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
			reconciler.trackDesired(newSS)

			fakeClient := fake.NewClientBuilder().Build()
			err := reconciler.ProcessResources(cr, fakeClient, nil)
			Expect(err).NotTo(HaveOccurred())

			var runAsRootOk = false
			for _, resource := range common.ToResourceList(reconciler.requestedResources) {
				if ss, ok := resource.(*appsv1.StatefulSet); ok {
					Expect(ss.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).NotTo(BeNil())
					Expect(*ss.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot).To(BeTrue())
					Expect(ss.Spec.Template.Spec.Containers[0].Image).NotTo(Equal(""))
					runAsRootOk = true
				}
			}
			Expect(runAsRootOk).To(BeTrue())
		})
	})

	It("process template custom attribute priority class name", Label(unitLabel), func() {
		kindMatchSs := "StatefulSet"

		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cr"},
			Spec: v1beta2.BrokerClusterSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{
						Selector: &v1beta2.ResourceSelector{
							Kind: &kindMatchSs,
						},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
							"spec": map[string]interface{}{
								"template": map[string]interface{}{
									"spec": map[string]interface{}{
										"priorityClassName": "high-priority",
									},
								},
							},
						},
						}),
					},
				},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		namer := MakeNamers(cr)

		newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
		reconciler.trackDesired(newSS)

		fakeClient := fake.NewClientBuilder().Build()
		err := reconciler.ProcessResources(cr, fakeClient, nil)
		Expect(err).NotTo(HaveOccurred())

		var priorityClassNameOk = false
		for _, resource := range common.ToResourceList(reconciler.requestedResources) {
			if ss, ok := resource.(*appsv1.StatefulSet); ok {
				Expect(ss.Spec.Template.Spec.PriorityClassName).To(Equal("high-priority"))
				priorityClassNameOk = true
			}
		}
		Expect(priorityClassNameOk).To(BeTrue())
	})

	It("new pod template spec for CR appends debug args", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				Env: []v1.EnvVar{
					{
						Name:  "DEBUG_ARGS",
						Value: "-Dtest.arg=foo",
					},
				},
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraMounts: v1beta2.ExtraMountsType{
						ConfigMaps: []string{
							"some-cm",
						},
						Secrets: []string{
							"test-config-jaas-config",
							"other",
						},
					},
				},
			},
		}

		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())
		expectedEnv := v1.EnvVar{
			Name:  "DEBUG_ARGS",
			Value: "-Dtest.arg=foo -Djava.security.auth.login.config=/amq/extra/secrets/test-config-jaas-config/login.config",
		}
		Expect(newSpec.Spec.Containers[0].Env).To(ContainElement(expectedEnv))
	})

	It("new pod template spec for CR includes image pull secret", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ImagePullSecrets: []v1.LocalObjectReference{
						{
							Name: "testPullSecret",
						},
					},
				},
			},
		}
		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())
		expectedPullSecret := []v1.LocalObjectReference{
			{
				Name: "testPullSecret",
			},
		}
		Expect(newSpec.Spec.ImagePullSecrets).To(Equal(expectedPullSecret))
	})

	It("new pod template spec for CR includes topology spread constraints", Label(unitLabel), func() {
		matchLabels := make(map[string]string)
		matchLabels["my-label"] = "my-value"

		mySelector := &metav1.LabelSelector{
			MatchLabels: matchLabels,
		}

		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					TopologySpreadConstraints: []v1.TopologySpreadConstraint{
						{
							MaxSkew:           int32(1),
							TopologyKey:       string("topology.kubernetes.io/zone"),
							WhenUnsatisfiable: v1.ScheduleAnyway,
							LabelSelector:     mySelector,
						},
					},
				},
			},
		}
		outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
		reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())
		expectedTopologySpreadConstraints := []v1.TopologySpreadConstraint{
			{
				MaxSkew:           int32(1),
				TopologyKey:       string("topology.kubernetes.io/zone"),
				WhenUnsatisfiable: v1.ScheduleAnyway,
				LabelSelector:     mySelector,
			},
		}
		Expect(newSpec.Spec.TopologySpreadConstraints).To(Equal(expectedTopologySpreadConstraints))
	})

	It("new pod template spec for CR includes container security context", Label(unitLabel), func() {
		containerSecurityContext := &v1.SecurityContext{RunAsNonRoot: pointer.To(false)}

		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ContainerSecurityContext: containerSecurityContext,
				},
			},
		}

		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: cr,
		}

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())
		expectedSecurityContext := &v1.SecurityContext{RunAsNonRoot: pointer.To(false)}

		Expect(newSpec.Spec.Containers[0].SecurityContext).To(Equal(expectedSecurityContext))
		Expect(newSpec.Spec.InitContainers[0].SecurityContext).To(Equal(expectedSecurityContext))
	})

	It("new pod template spec for CR includes extra volumes", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraVolumes: []v1.Volume{
						{
							Name: "my-extra-volume",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		}

		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: cr,
		}

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())

		mainContainerHasMount := false
		for _, mount := range newSpec.Spec.Containers[0].VolumeMounts {
			if mount.Name == "my-extra-volume" {
				mainContainerHasMount = true
				Expect(mount.MountPath).To(Equal("/amq/extra/volumes/my-extra-volume"))
				break
			}
		}
		Expect(mainContainerHasMount).To(BeTrue(), "Main container should have the extra volume mount")

		Expect(newSpec.Spec.InitContainers).NotTo(BeEmpty(), "Should have init containers")
		initContainerHasMount := false
		for _, mount := range newSpec.Spec.InitContainers[0].VolumeMounts {
			if mount.Name == "my-extra-volume" {
				initContainerHasMount = true
				Expect(mount.MountPath).To(Equal("/amq/extra/volumes/my-extra-volume"))
				break
			}
		}
		Expect(initContainerHasMount).To(BeTrue(), "Init container should have the extra volume mount")
	})

	It("new pod template spec for CR includes extra volumes with custom mount", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraVolumes: []v1.Volume{
						{
							Name: "my-extra-volume",
							VolumeSource: v1.VolumeSource{
								EmptyDir: &v1.EmptyDirVolumeSource{},
							},
						},
					},
					ExtraVolumeMounts: []v1.VolumeMount{
						{
							Name:      "my-extra-volume",
							MountPath: "/custom/path",
						},
					},
				},
			},
		}

		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: cr,
		}

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())

		mainContainerHasMount := false
		for _, mount := range newSpec.Spec.Containers[0].VolumeMounts {
			if mount.Name == "my-extra-volume" {
				mainContainerHasMount = true
				Expect(mount.MountPath).To(Equal("/custom/path"))
				break
			}
		}
		Expect(mainContainerHasMount).To(BeTrue(), "Main container should have the extra volume mount")

		Expect(newSpec.Spec.InitContainers).NotTo(BeEmpty(), "Should have init containers")
		initContainerHasMount := false
		for _, mount := range newSpec.Spec.InitContainers[0].VolumeMounts {
			if mount.Name == "my-extra-volume" {
				initContainerHasMount = true
				Expect(mount.MountPath).To(Equal("/custom/path"))
				break
			}
		}
		Expect(initContainerHasMount).To(BeTrue(), "Init container should have the extra volume mount with custom path")
	})

	It("new pod template spec for CR includes extra volume claim templates", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			Spec: v1beta2.BrokerClusterSpec{
				DeploymentPlan: v1beta2.DeploymentPlanType{
					ExtraVolumeClaimTemplates: []v1beta2.VolumeClaimTemplate{
						{
							ObjectMeta: v1beta2.ObjectMeta{
								Name: "my-pvc",
							},
							Spec: v1.PersistentVolumeClaimSpec{
								AccessModes: []v1.PersistentVolumeAccessMode{
									v1.ReadWriteOnce,
								},
							},
						},
					},
				},
			},
		}

		reconciler := &BrokerClusterReconcilerImpl{
			log:            ctrl.Log.WithName("test"),
			customResource: cr,
		}

		newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

		Expect(err).NotTo(HaveOccurred())
		Expect(newSpec).NotTo(BeNil())

		mainContainerHasMount := false
		for _, mount := range newSpec.Spec.Containers[0].VolumeMounts {
			if mount.Name == "my-pvc" {
				mainContainerHasMount = true
				Expect(mount.MountPath).To(Equal("/opt/my-pvc/data"))
				break
			}
		}
		Expect(mainContainerHasMount).To(BeTrue(), "Main container should have the extra PVC mount")

		Expect(newSpec.Spec.InitContainers).NotTo(BeEmpty(), "Should have init containers")
		initContainerHasMount := false
		for _, mount := range newSpec.Spec.InitContainers[0].VolumeMounts {
			if mount.Name == "my-pvc" {
				initContainerHasMount = true
				Expect(mount.MountPath).To(Equal("/opt/my-pvc/data"))
				break
			}
		}
		Expect(initContainerHasMount).To(BeTrue(), "Init container should have the extra PVC mount")
	})

	It("login config syntax check", Label(unitLabel), func() {
		good := map[string][]byte{
			"simple": []byte(`a {
		SampleLoginModule Required  a=b b=d;
		SampleLoginModule Optional;
		SampleLoginModule requisite;
		SampleLoginModule sufficient;
	   };`),
			"equalsSpace": []byte(`a {
		SampleLoginModule Required  a = b b= d c = 4;
		SampleLoginModule Optional
		a =2
		b= 3
		c = 4
		;
	   };`),

			"quotex": []byte(` aaa {
		SampleLoginModule Required  a=b b=d;
		SampleLoginModule Required
		   base=2
		   option=x;
	   };`),
			"comments": []byte(` aaa {
		// a good comment
		/* and another */
		/* and line */
		SampleLoginModule Required  a=b b=d;
		// more comments
		SampleLoginModule Required
		   base=2
		   option=x;
	   };`),

			"comments_multiline": []byte(` aaa {
		/* and multi
		line */
		SampleLoginModule Required  a=b b=d;

		/* more
		comments */

	   };`),

			"comment_at_end_of_line": []byte(` aaa {
		// a good comment
		SampleLoginModule Required  a=b b=d; // again
		SampleLoginModule Required
		   base=2
		   option=x; // and another comment
	   };`),

			"twoRealm": []byte(` aa
		{
		SampleLoginModule Required  a=b b=d;
		SampleLoginModule Required
		   base=2
		   option="x";
	   };

	     bb {
		   SampleLoginModule Required
		   base=2
		   option="${x}";
	 }  ;`),

			"full": []byte(`
	 // a full login.config
	 activemq {
		 org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule required
			 reload=true
			 debug=true
			 org.apache.activemq.jaas.properties.user="users.properties"
			 org.apache.activemq.jaas.properties.role="roles.properties";
	 };

	 console {

		 // ensure the operator can connect to the mgmt console by referencing the existing properties config
		 // operatorAuth = plain
		 // hawtio.realm = console
		 org.apache.activemq.artemis.spi.core.security.jaas.PropertiesLoginModule required
			 reload=true
			 debug=true
			 org.apache.activemq.jaas.properties.user="artemis-users.properties"
			 org.apache.activemq.jaas.properties.role="artemis-roles.properties"
			 baseDir="/home/jboss/amq-broker/etc";

	 };`),

			"full-ldap-quoted-val": []byte(`
	 activemq	{ org.apache.activemq.artemis.spi.core.security.jaas.LDAPLoginModule sufficient
		debug=true
		initialContextFactory=com.sun.jndi.ldap.LdapCtxFactory
		connectionURL="ldap://blabla"
		connectionTimeout="5000"
		connectionProtocol="simple"
		readTimeout="5000"
		authentication="simple"
		userBase="DC=aa,DC=aaa,DC=aaaaa,DC=aa,DC=aa"
		userSearchMatching="(&(objectCategory=user)(SAMAccountName=\\{0}))"
		roleBase="DC=aa,DC=aaa,DC=aaaaa,DC=aa,DC=aa"
		roleName=sAMAccountName
		roleSearchMatching="(&(objectCategory=group)(groupType:1.1.111.111111.1.1.111:=1111111111)(member:1.1.111.111111.1.1.1111:=\{0})(sAMAccountName=AAA AAA AAA*))"
		referral=follow;
	 };`),
		}

		for k, v := range good {
			Expect(MatchBytesAgainsLoginConfigRegexp(v)).To(BeTrue(), "for key "+k)
		}

		bad := map[string][]byte{
			"twoRealm-missingSemiBetweenRealms": []byte(` aa
		{
		SampleLoginModule Required  a=b b=d;
		SampleLoginModule Required
		   base=2
		   option="x";
	   } // missing semi - and comments! // may have to strip comments as a first step of validation

	     bb {
		   SampleLoginModule Required
		   base=2
		   option="${x}";
	 }  ;`),
			"no_flags": []byte(`aa
	 {
	     SampleLoginModule a=b b=d;
	 };`),

			"dual_munged_opt": []byte(`aa
	 {
	     SampleLoginModule sufficientRequired a=b;
	 };`),

			"dual_or_opt": []byte(`aa
	 {
	     SampleLoginModule Sufficient|Required a=b;
	 };`),

			"dual_space_opt": []byte(`aa
	 {
	     SampleLoginModule Sufficient Required a=b;
	 };`),

			"no_semi_on_module": []byte(`aa
	 {
	     SampleLoginModule sufficient a=b
	 };`),

			"no_semi_at_end": []byte(`aa
	 {
	     SampleLoginModule sufficient;
	 }`),
			"no_value_for_key": []byte(`aa
	 {
	     SampleLoginModule sufficient a=;
	 };`),
			"no_key for value": []byte(`aa
	 {
	     SampleLoginModule sufficient =a;
	 };`),
		}

		for k, v := range bad {
			Expect(MatchBytesAgainsLoginConfigRegexp(v)).To(BeFalse(), "for key "+k)
		}
	})

	It("status marshall", Label(unitLabel), func() {
		Status := v1beta2.BrokerClusterStatus{
			Conditions: []metav1.Condition{},
			PodStatus: olm.DeploymentStatus{
				Ready:    []string{},
				Starting: []string{},
				Stopped:  []string{},
			},
			DeploymentPlanSize: 0,
			ScaleLabelSelector: "",
			ExternalConfigs:    []v1beta2.ExternalConfigStatus{},
			Version:            v1beta2.VersionStatus{},
			Upgrade:            v1beta2.UpgradeStatus{},
		}
		v, err := json.Marshal(Status)
		Expect(err).To(BeNil())
		Expect(strings.Contains(string(v), ":false")).To(BeTrue())
	})

	It("get broker host", Label(unitLabel), func() {
		cr := v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test",
			},
			Spec: v1beta2.BrokerClusterSpec{
				IngressDomain: "my-domain.com",
			},
		}

		var ingressHost string
		specIngressHost := "$(CR_NAME)-$(CR_NAMESPACE)-$(ITEM_NAME)-$(BROKER_ORDINAL)-$(RES_TYPE).$(INGRESS_DOMAIN)"

		ingressHost = formatTemplatedString(&cr, specIngressHost, "0", "my-acceptor", "ing")
		Expect(ingressHost).To(Equal("test-test-ns-my-acceptor-0-ing.my-domain.com"))

		ingressHost = formatTemplatedString(&cr, specIngressHost, "1", "my-connector", "rte")
		Expect(ingressHost).To(Equal("test-test-ns-my-connector-1-rte.my-domain.com"))

		ingressHost = formatTemplatedString(&cr, specIngressHost, "2", "my-console", "abc")
		Expect(ingressHost).To(Equal("test-test-ns-my-console-2-abc.my-domain.com"))
	})

	It("format templated string with invalid variables", Label(unitLabel), func() {
		cr := v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test",
			},
			Spec: v1beta2.BrokerClusterSpec{
				IngressDomain: "my-domain.com",
			},
		}

		Expect(formatTemplatedString(&cr, "test-$(UNKNOWN_VAR)", "", "", "")).To(Equal("test-$(UNKNOWN_VAR)"))
		Expect(formatTemplatedString(&cr, "prefix-$(CR_NAME)-$(INVALID)-suffix", "0", "", "")).To(Equal("prefix-test-$(INVALID)-suffix"))
	})

	It("format templated object", Label(unitLabel), func() {
		cr := v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test",
			},
			Spec: v1beta2.BrokerClusterSpec{
				IngressDomain: "my-domain.com",
			},
		}

		templatedValue := "$(CR_NAME)-$(CR_NAMESPACE)-$(ITEM_NAME)-$(BROKER_ORDINAL)-$(RES_TYPE).$(INGRESS_DOMAIN)"
		templatedObject := map[string]interface{}{
			"plain-string": "TEST",
			"plain-int":    1,
			"test-map": map[string]interface{}{
				"test-map-key":        templatedValue,
				"nested-plain-string": "TEST",
				"nested-plain-int":    1,
				"nested-test-array": []interface{}{
					templatedValue,
				},
			},
			"test-array": []interface{}{
				templatedValue,
				map[string]interface{}{
					"nested-test-map-key": templatedValue,
				},
			},
			"test-string": templatedValue,
		}

		var formattedObject map[string]interface{}

		formattedObject = formatTemplatedObject(&cr, templatedObject, "0", "test-name-a", "test-type-A").(map[string]interface{})
		testFormattedObject(formattedObject, "test-test-ns-test-name-a-0-test-type-A.my-domain.com")

		formattedObject = formatTemplatedObject(&cr, templatedObject, "1", "test-name-b", "test-type-B").(map[string]interface{})
		testFormattedObject(formattedObject, "test-test-ns-test-name-b-1-test-type-B.my-domain.com")
	})

	Context("ensure owner reference API version", func() {
		It("no owner references", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret", OwnerReferences: []metav1.OwnerReference{}},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeTrue(), "should return true when no owner references exist")
			Expect(candidate.GetOwnerReferences()).To(BeEmpty(), "candidate owner references should not be modified")
		})

		It("matching API version", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "broker.amq.io/v1beta1", Kind: "ActiveMQArtemis", Name: "test-broker", UID: "test-uid"},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeTrue(), "should return true when API versions match")
			Expect(candidate.GetOwnerReferences()).To(BeEmpty(), "candidate owner references should not be modified when versions match")
		})

		It("different API version", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "broker.amq.io/v1alpha1", Kind: "ActiveMQArtemis", Name: "test-broker", UID: "test-uid"},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeFalse(), "should return false when API versions differ")
			Expect(candidate.GetOwnerReferences()).To(HaveLen(1), "candidate should have owner references set")
			Expect(candidate.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "candidate should have updated API version")
		})

		It("multiple owner references", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "apps/v1", Kind: "Deployment", Name: "other-owner", UID: "other-uid"},
						{APIVersion: "broker.amq.io/v1alpha1", Kind: "ActiveMQArtemis", Name: "test-broker", UID: "test-uid"},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeFalse(), "should return false when ActiveMQArtemis owner reference API version differs")
			Expect(candidate.GetOwnerReferences()).To(HaveLen(2), "candidate should have both owner references")
			Expect(candidate.GetOwnerReferences()[0].APIVersion).To(Equal("apps/v1"), "first owner reference should remain unchanged")
			Expect(candidate.GetOwnerReferences()[1].APIVersion).To(Equal("broker.amq.io/v1beta1"), "ActiveMQArtemis owner reference should be updated")
		})

		It("different broker name", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "broker.amq.io/v1alpha1", Kind: "ActiveMQArtemis", Name: "different-broker", UID: "test-uid"},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret"},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeTrue(), "should return true when owner reference is for a different broker")
			Expect(candidate.GetOwnerReferences()).To(BeEmpty(), "candidate owner references should not be modified")
		})
	})

	It("compare secret with API version update", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
		}

		deployed := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-secret", Namespace: "test-ns",
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "broker.amq.io/v1alpha1", Kind: "ActiveMQArtemis", Name: "test-broker", UID: "test-uid"},
				},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}

		requested := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "test-ns"},
			Data:       map[string][]byte{"key": []byte("value")},
		}

		r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
		ri := NewBrokerClusterReconcilerImpl(cr, r)

		result := ri.CompareSecret(deployed, requested)

		Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
		Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
		Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
	})

	It("compare config map with API version update", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
		}

		deployed := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-config", Namespace: "test-ns",
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "broker.amq.io/v1alpha1", Kind: "ActiveMQArtemis", Name: "test-broker", UID: "test-uid"},
				},
			},
		}

		requested := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "test-ns"},
		}

		r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
		ri := NewBrokerClusterReconcilerImpl(cr, r)

		result := ri.CompareConfigMap(deployed, requested)

		Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
		Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
		Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
	})

	It("compare meta and spec with API version update", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			TypeMeta:   metav1.TypeMeta{APIVersion: "broker.amq.io/v1beta1"},
			ObjectMeta: metav1.ObjectMeta{Name: "test-broker", Namespace: "test-ns"},
		}

		deployed := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ss", Namespace: "test-ns",
				Labels: map[string]string{"app": "test"},
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "broker.amq.io/v1alpha1", Kind: "ActiveMQArtemis", Name: "test-broker", UID: "test-uid"},
				},
			},
			Spec: appsv1.StatefulSetSpec{Replicas: pointer.To(int32(1))},
		}

		requested := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ss", Namespace: "test-ns",
				Labels: map[string]string{"app": "test"},
			},
			Spec: appsv1.StatefulSetSpec{Replicas: pointer.To(int32(1))},
		}

		r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false)
		ri := NewBrokerClusterReconcilerImpl(cr, r)

		result := ri.CompareMetaAndSpec(deployed, requested)

		Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
		Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
		Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
	})

	It("make namers uses ActiveMQArtemis tracking label", Label(unitLabel), func() {
		cr := &v1beta2.BrokerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "ex-aao"},
		}

		namer := MakeNamers(cr)
		labels := namer.LabelBuilder.Labels()

		Expect(labels[selectors.LabelActiveMQArtemisKey]).To(Equal("ex-aao"))
		Expect(labels[selectors.LabelAppKey]).To(Equal("ex-aao-app"))
		Expect(labels[selectors.LabelPartOfKey]).To(Equal(selectors.LabelPartOfValue))
		_, hasBroker := labels[selectors.LabelBrokerKey]
		Expect(hasBroker).To(BeFalse())
	})

	Context("new projection from string values", func() {
		It("properties file", Label(unitLabel), func() {
			resourceMeta := metav1.ObjectMeta{
				Name:            "test-config",
				ResourceVersion: "12345",
				Generation:      3,
			}

			data := map[string]string{
				"address.properties": "addressConfigurations.myQueue.routingTypes=ANYCAST\n",
			}

			proj := newProjectionFromStringValues(resourceMeta, data)

			Expect(proj.Name).To(Equal("test-config"))
			Expect(proj.ResourceVersion).To(Equal("12345"))
			Expect(proj.Generation).To(Equal(int64(3)))
			Expect(proj.Files).To(HaveKey("address.properties"))
			Expect(proj.Files["address.properties"].Alder32).NotTo(BeEmpty())
			Expect(proj.Files["address.properties"].FileAlder32).NotTo(BeEmpty())
		})

		It("json file", Label(unitLabel), func() {
			resourceMeta := metav1.ObjectMeta{
				Name:            "test-config",
				ResourceVersion: "12345",
				Generation:      3,
			}

			data := map[string]string{
				"broker.json": `{"key":"value"}`,
			}

			proj := newProjectionFromStringValues(resourceMeta, data)

			Expect(proj.Files).To(HaveKey("broker.json"))
			Expect(proj.Files["broker.json"].Alder32).NotTo(BeEmpty())
			Expect(proj.Files["broker.json"].FileAlder32).NotTo(BeEmpty())
		})

		It("invalid json file is skipped", Label(unitLabel), func() {
			resourceMeta := metav1.ObjectMeta{
				Name:            "test-config",
				ResourceVersion: "12345",
				Generation:      3,
			}

			data := map[string]string{
				"broker.json": `{"key":`,
			}

			proj := newProjectionFromStringValues(resourceMeta, data)

			Expect(proj.Files).NotTo(HaveKey("broker.json"))
		})

		It("matches newProjectionFromByteValues", Label(unitLabel), func() {
			resourceMeta := metav1.ObjectMeta{
				Name:            "test-config",
				ResourceVersion: "12345",
				Generation:      3,
			}

			stringData := map[string]string{
				"address.properties": "addressConfigurations.myQueue.routingTypes=ANYCAST\n",
				"broker.json":        `{"key":"value"}`,
			}

			byteData := make(map[string][]byte, len(stringData))
			for k, v := range stringData {
				byteData[k] = []byte(v)
			}

			projString := newProjectionFromStringValues(resourceMeta, stringData)
			projBytes := newProjectionFromByteValues(resourceMeta, byteData)

			Expect(projBytes.Name).To(Equal(projString.Name))
			Expect(projBytes.ResourceVersion).To(Equal(projString.ResourceVersion))
			Expect(projBytes.Generation).To(Equal(projString.Generation))

			for fileName := range stringData {
				Expect(projBytes.Files[fileName].Alder32).To(Equal(projString.Files[fileName].Alder32), "Alder32 mismatch for %s", fileName)
				Expect(projBytes.Files[fileName].FileAlder32).To(Equal(projString.Files[fileName].FileAlder32), "FileAlder32 mismatch for %s", fileName)
			}
		})

		It("empty data", Label(unitLabel), func() {
			resourceMeta := metav1.ObjectMeta{
				Name:            "test-config",
				ResourceVersion: "12345",
				Generation:      3,
			}

			proj := newProjectionFromStringValues(
				resourceMeta,
				map[string]string{},
			)

			Expect(proj.Name).To(Equal("test-config"))
			Expect(proj.ResourceVersion).To(Equal("12345"))
			Expect(proj.Generation).To(Equal(int64(3)))
			Expect(proj.Files).To(BeEmpty())
		})
	})
})
