package controllers

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes/scheme"
	pointer "k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestHexShaHashOfMap(t *testing.T) {

	nilOne := hexShaHashOfMap(nil)
	nilTwo := hexShaHashOfMap(nil)

	if nilOne != nilTwo {
		t.Errorf("HexShaHashOfMap(nil) = %v, want %v", nilOne, nilTwo)
	}

	props := []string{"a=a", "b=b"}

	propsOriginal := hexShaHashOfMap(props)

	// modify
	props = append(props, "c=c")

	propsModified := hexShaHashOfMap(props)

	if propsOriginal == propsModified {
		t.Errorf("HexShaHashOfMap(props mod) = %v, want %v", propsOriginal, propsModified)
	}

	// revert, drop the last entry b/c they are ordered
	props = props[:2]

	if propsOriginal != hexShaHashOfMap(props) {
		t.Errorf("HexShaHashOfMap(props) with revert = %v, want %v", propsOriginal, hexShaHashOfMap(props))
	}

	// modify further, drop first entry
	props = props[:1]

	if propsOriginal == hexShaHashOfMap(props) {
		t.Errorf("HexShaHashOfMap(props) with just a = %v, want %v", propsOriginal, hexShaHashOfMap(props))
	}

}

func TestMapComparatorForStatefulSet(t *testing.T) {

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

	for resourceType, delta := range deltas {
		t.Log("", "instances of ", resourceType, "Will create ", len(delta.Added), "update ", len(delta.Updated), "and delete", len(delta.Removed))

		for index := range delta.Added {
			resourceToAdd := delta.Added[index]
			t.Log("", "instances of ", resourceType, "Will add", resourceToAdd)
		}

		for index := range delta.Updated {
			resourceToUpdate := delta.Updated[index]
			t.Log("", "instances of ", resourceType, "Will update", resourceToUpdate)

		}

		for index := range delta.Removed {
			resourceToRemove := delta.Removed[index]
			t.Log("", "instances of ", resourceType, "Will remove", resourceToRemove)

		}
	}
	if len(deltas[ssType].Added) != 1 {
		t.Errorf("expect new addition to appear!")

	}
	if (len(deltas[ssType].Updated)) != 1 {
		t.Errorf("not good!, expect difference on ss to be respected as an update")
	}
}

func TestComparatorMetaAndSpec(t *testing.T) {

	reconciler := &BrokerClusterReconcilerImpl{
		log:            ctrl.Log.WithName("test"),
		customResource: nil,
	}

	ss0 := &appsv1.StatefulSet{}
	equal := reconciler.CompareMetaAndSpec(ss0, ss0)

	if !equal {
		t.Errorf("expect equal on same instance!")
	}

	ss1 := &appsv1.StatefulSet{}
	ss1.Annotations = map[string]string{"A": "B"}
	equal = reconciler.CompareMetaAndSpec(ss0, ss1)

	if equal {
		t.Errorf("expect not equal on differnet annotations")
	}
}

func TestGetSingleStatefulSetStatus(t *testing.T) {

	var expected int32 = int32(1)
	ss := &appsv1.StatefulSet{}
	ss.ObjectMeta.Name = "joe"
	ss.Spec.Replicas = &expected
	ss.Status.Replicas = 1
	ss.Status.ReadyReplicas = 1

	cr := &v1beta2.BrokerCluster{}
	statusRunning := common.GetSingleStatefulSetStatus(ss, cr)
	if statusRunning.Ready[0] != "joe-0" {
		t.Errorf("not good!, expect correct 0 ordinal %s", statusRunning.Ready[0])
	}

	ss.Status.Replicas = 0
	ss.Status.ReadyReplicas = 0

	statusRunning = common.GetSingleStatefulSetStatus(ss, cr)
	if statusRunning.Stopped[0] != "joe" {
		t.Errorf("not good!, expect ss name in stopped %s", statusRunning.Stopped[0])
	}

	var expectedTwo int32 = int32(2)
	ss.Spec.Replicas = &expectedTwo
	ss.Status.Replicas = 2
	ss.Status.ReadyReplicas = 1

	statusRunning = common.GetSingleStatefulSetStatus(ss, cr)
	if statusRunning.Ready[0] != "joe-0" {
		t.Errorf("not good!, expect correct 0 ordinal ready %s", statusRunning.Ready[0])
	}

	if statusRunning.Starting[0] != "joe-1" {
		t.Errorf("not good!, expect ss name in starting %s", statusRunning.Stopped[0])
	}

	if cr.Status.DeploymentPlanSize != 2 {
		t.Errorf("not good!, status not updated")
	}
}

func TestGetConfigAppliedConfigMapName(t *testing.T) {
	cr := v1beta2.BrokerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test",
		},
	}
	name := getPropertiesResourceNsName(&cr)
	assert.Equal(t, "test-ns", name.Namespace)
	assert.Equal(t, "test-props", name.Name)
}

func TestExtractSha(t *testing.T) {
	json := `{"configuration": {"properties": {"a_status.properties": {"alder32": "123456"}}}}`
	status, err := unmarshallStatus(json)
	assert.NoError(t, err)
	sha, err := extractSha(status, "a_status.properties")
	assert.Equal(t, "123456", sha)
	assert.NoError(t, err)

	json = `{"configuration": {"properties": {"a_status.properties": {}}}}`
	status, err = unmarshallStatus(json)
	assert.NoError(t, err)
	sha, err = extractSha(status, "a_status.properties")
	assert.Empty(t, sha)
	assert.NoError(t, err)

	json = `you shall fail`
	status, err = unmarshallStatus(json)
	assert.Error(t, err)
	sha, err = extractSha(status, "a_status.properties")
	assert.Empty(t, sha)
	assert.Error(t, err)
}

func extractSha(status brokerStatus, name string) (string, error) {
	current, present := status.BrokerConfigStatus.PropertiesStatus[name]
	if !present {
		return "", errors.New("not present")
	} else {
		return current.Alder32, nil
	}
}

func TestAlder32Gen(t *testing.T) {

	userProps := `admin=admin
			tom=tom
			peter=peter`

	res := alder32FromData([]byte(userProps))
	assert.True(t, strings.Contains(res, "2905476010"))
}

func TestAlder32GenSpace(t *testing.T) {

	userProps := `admin = joe`

	res := alder32FromData([]byte(userProps))
	assert.True(t, strings.Contains(res, "295568261"))
}

func TestAlder32GenWithEmptyLine(t *testing.T) {

	userProps := `
			admin=admin
			tom=tom
			peter=peter`

	res := alder32FromData([]byte(userProps))
	assert.True(t, strings.Contains(res, "2905476010"))
}

func TestAlder32GenWithSpace(t *testing.T) {

	userProps := `addressesSettings.#.redeliveryMultiplier=2.3
	addressesSettings.#.redeliveryCollisionAvoidanceFactor=1.2
	addressesSettings.Some\ value\ with\ space.redeliveryCollisionAvoidanceFactor=1.2`

	res := alder32FromData([]byte(userProps))
	assert.EqualValues(t, "2211202255", res)
}

func TestAlder32GenWithDots(t *testing.T) {

	userProps := `addressSettings.#.redeliveryMultiplier=5
    addressSettings.\"news.#\".redeliveryMultiplier=2
    addressSettings.\"order.#\".redeliveryMultiplier=3`

	res := alder32FromData([]byte(userProps))
	assert.EqualValues(t, "3264295767", res)
}

func TestAlder32GenBrokerProps(t *testing.T) {

	propsString := "# generated by crd\n#\nconnectionRouters.autoShard.keyType=CLIENT_ID\nconnectionRouters.autoShard.localTargetFilter=NULL|${STATEFUL_SET_ORDINAL}|-${STATEFUL_SET_ORDINAL}\nconnectionRouters.autoShard.policyConfiguration=CONSISTENT_HASH_MODULO\nconnectionRouters.autoShard.policyConfiguration.properties.MODULO=2\nacceptorConfigurations.tcp.params.router=autoShard\naddressesSettings.\"LB.#\".defaultAddressRoutingType=ANYCAST\n"

	res := alder32FromData([]byte(propsString))
	assert.True(t, strings.Contains(res, "1897435425"))
}

func TestAlder32RolesProps(t *testing.T) {

	propsStringWithLeadingWhiteSpaceBeforeComment := `
	# rbac
    control-plane=control-plane,control-plane-0,control-plane-1
    consumers=c1,c2,c3,c4
    producers=p
	! exclimation mark comment to strip with leading ws
! as start of line to strip
     # partitioned consumer roles for connectionRouter
shard-consumers-broker-0=c1,c2
shard-consumers-broker-1=c3,c4

     		# should resolve to NULL in absence of this
shard-control-plane=control-plane,control-plane-0,control-plane-1
shard-producers=p`

	propsStringCommentsStripped := `
control-plane=control-plane,control-plane-0,control-plane-1
consumers=c1,c2,c3,c4
producers=p
shard-consumers-broker-0=c1,c2
shard-consumers-broker-1=c3,c4
shard-control-plane=control-plane,control-plane-0,control-plane-1
shard-producers=p`

	res := alder32FromData([]byte(propsStringWithLeadingWhiteSpaceBeforeComment))

	expected := alder32FromData([]byte(propsStringCommentsStripped))

	assert.Equal(t, res, expected)
}

func TestAlder32PropsWithFF(t *testing.T) {

	propsStringWithLeadingWhiteSpaceBeforeComment := "\n\t\f# with form feed\nproducers=p"

	propsStringCommentsStripped := "producers=p"

	res := alder32FromData([]byte(propsStringWithLeadingWhiteSpaceBeforeComment))

	expected := alder32FromData([]byte(propsStringCommentsStripped))

	assert.Equal(t, res, expected)
}

func TestExtractErrors(t *testing.T) {

	json := "{\"configuration\":{\"properties\":{\"broker.properties\":{\"alder32\":\"1\"},\"system\":{\"alder32\":\"1\"}}},\"server\":{\"jaas\":{\"properties\":{\"artemis-users.properties\":{\"reloadTime\":\"1669744377685\",\"Alder32\":\"955331033\"},\"artemis-roles.properties\":{\"reloadTime\":\"1669744377685\",\"Alder32\":\"701302135\"}}},\"state\":\"STARTED\",\"version\":\"2.27.0\",\"nodeId\":\"a644c0c6-700e-11ed-9d4f-0a580ad90188\",\"identity\":null,\"uptime\":\"33.176 seconds\"}}"
	status, err := unmarshallStatus(json)
	assert.NoError(t, err)
	sha, err := extractSha(status, "broker.properties")
	assert.Equal(t, "1", sha)
	assert.NoError(t, err)

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
	assert.NoError(t, err)
	appplyErrors := status.BrokerConfigStatus.PropertiesStatus["broker.properties"].ApplyErrors
	assert.True(t, len(appplyErrors) > 0)

	marshalledErrorsStr := marshallApplyErrors(appplyErrors)
	assert.True(t, strings.Contains(marshalledErrorsStr, "bla"))

}

func TestGetJaasConfigExtraMountPath(t *testing.T) {
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
	path, found := getJaasConfigExtraMountPath(cr)
	assert.Equal(t, path, "/amq/extra/secrets/test-config-jaas-config/login.config")
	assert.True(t, found)

	cr = &v1beta2.BrokerCluster{
		Spec: v1beta2.BrokerClusterSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				ExtraMounts: v1beta2.ExtraMountsType{
					ConfigMaps: []string{
						"test-config",
						"some-cm",
					},
					Secrets: []string{
						"test-config-jaas-config",
						"other-secret",
					},
				},
			},
		},
	}
	path, found = getJaasConfigExtraMountPath(cr)
	assert.Equal(t, path, "/amq/extra/secrets/test-config-jaas-config/login.config")
	assert.True(t, found)
}

func TestGetJaasConfigExtraMountPathNotPresent(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		Spec: v1beta2.BrokerClusterSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				ExtraMounts: v1beta2.ExtraMountsType{
					ConfigMaps: []string{
						"test-config",
					},
				},
			},
		},
	}
	path, found := getJaasConfigExtraMountPath(cr)
	assert.Empty(t, path)
	assert.False(t, found)
}

func TestNewPodTemplateSpecForCR_IncludesDebugArgs(t *testing.T) {

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

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedEnv := v1.EnvVar{
		Name:  "DEBUG_ARGS",
		Value: "-Djava.security.auth.login.config=/amq/extra/secrets/test-config-jaas-config/login.config",
	}
	assert.Contains(t, newSpec.Spec.Containers[0].Env, expectedEnv)
}

func TestProcess_TemplateIncludesLabelsServiceAndSecret(t *testing.T) {

	var kindMatch string = "Secret"
	cr := &v1beta2.BrokerCluster{
		Spec: v1beta2.BrokerClusterSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				Labels: map[string]string{"myPodKey": "myPodValue"},
			},
			ResourceTemplates: []v1beta2.ResourceTemplate{
				{
					// match all
					Labels: map[string]string{"myKey": "myValue"},
				},
				{
					// match just Secrets
					Selector: &v1beta2.ResourceSelector{
						Kind: &kindMatch,
					},
					Labels: map[string]string{"mySecretKey": "mySecretValue"},
				}},
		},
	}
	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("TestProcess_TemplateIncludesLabelsServiceAndSecret"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, err := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.trackDesired(newSS)
	assert.NoError(t, err)

	reconciler.ProcessDeploymentPlan(cr, *namer, nil, nil, newSS)

	fakeClient := fake.NewClientBuilder().Build()
	err = reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var ssFound = false
	var secretFound = false
	var serviceFound = false
	for _, resource := range common.ToResourceList(reconciler.requestedResources) {
		if ss, ok := resource.(*appsv1.StatefulSet); ok {

			newSpec := ss.Spec.Template
			assert.NoError(t, err)
			assert.NotNil(t, newSpec)

			v, ok := newSpec.Labels["myPodKey"]
			assert.True(t, ok)
			assert.Equal(t, "myPodValue", v)

			ssFound = true
		}

		if secret, ok := resource.(*v1.Secret); ok {
			assert.True(t, len(secret.Labels) >= 1)
			assert.Equal(t, secret.Labels["myKey"], "myValue")
			assert.Equal(t, secret.Labels["mySecretKey"], "mySecretValue")
			secretFound = true
		}

		if service, ok := resource.(*v1.Service); ok {
			assert.True(t, len(service.Labels) >= 1)
			assert.Equal(t, service.Labels["myKey"], "myValue")
			_, found := service.Labels["mySecretKey"]
			assert.False(t, found)
			serviceFound = true
		}
	}
	assert.True(t, ssFound)
	assert.True(t, secretFound)
	assert.True(t, serviceFound)
}

func TestProcess_TemplateIncludesLabelsSecretRegexp(t *testing.T) {

	var regexpNameMatch string = ".*-props"
	var exactNameMatch string = "-props"

	cr := &v1beta2.BrokerCluster{
		Spec: v1beta2.BrokerClusterSpec{
			ResourceTemplates: []v1beta2.ResourceTemplate{
				{
					Selector: &v1beta2.ResourceSelector{
						// match just -props secrets by name with regex
						Name: &regexpNameMatch,
					},
					Labels: map[string]string{"mySecretKey": "mySecretValue"},
				},
				{
					Selector: &v1beta2.ResourceSelector{
						// match just -props secrets by name
						Name: &exactNameMatch,
					},
					Labels: map[string]string{"myExactSecretKey": "myExactSecretValue"},
				}},
		},
	}

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("TestProcess_TemplateIncludesLabelsServiceAndSecret"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.ProcessDeploymentPlan(cr, *namer, nil, nil, newSS)

	fakeClient := fake.NewClientBuilder().Build()
	err := reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var secretFound = false
	var serviceFound = false

	for _, resource := range common.ToResourceList(reconciler.requestedResources) {
		if secret, ok := resource.(*v1.Secret); ok {
			assert.True(t, len(secret.Labels) >= 1)
			assert.Equal(t, secret.Labels["mySecretKey"], "mySecretValue")
			assert.Equal(t, secret.Labels["myExactSecretKey"], "myExactSecretValue")
			secretFound = true
		}

		if service, ok := resource.(*v1.Service); ok {
			assert.True(t, len(service.Labels) >= 1)
			_, found := service.Labels["mySecretKey"]
			assert.False(t, found)
			_, found = service.Labels["myExactSecretKey"]
			assert.False(t, found)
			serviceFound = true
		}

	}
	assert.True(t, secretFound)
	assert.True(t, serviceFound)

}

func TestProcess_TemplateDuplicateKeyReplacesOk(t *testing.T) {

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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("TestProcess_TemplateDuplicateKeyReplacesOk"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.ProcessDeploymentPlan(cr, *namer, nil, nil, newSS)

	fakeClient := fake.NewClientBuilder().Build()
	err := reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var secretFound = false
	for _, resource := range common.ToResourceList(reconciler.requestedResources) {
		if secret, ok := resource.(*v1.Secret); ok {
			assert.True(t, len(secret.Labels) >= 1)
			assert.Equal(t, secret.Labels["mySecretKey"], "mySecretValue")
			secretFound = true
		}
	}
	assert.True(t, secretFound)
}

func Test_Respect_existing_JAVA_OPTS_properties_def(t *testing.T) {

	cr := &v1beta2.BrokerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cr"},
		Spec:       v1beta2.BrokerClusterSpec{},
	}

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("Test_Respect_existing_JAVA_OPTS_properties_def"), isOpenshift)
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
	assert.True(t, index != -1)
	assert.True(t, strings.Contains(newSS.Spec.Template.Spec.InitContainers[0].Env[index].Value, "properties"))
}

func TestProcess_TemplateKeyValue(t *testing.T) {

	var kindMatch string = "Service"
	var matchOrdinalServices string = ".+-[0-9]+-svc"
	var matchGvForIngress string = "networking.k8s.io/v1"
	cr := &v1beta2.BrokerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cr"},
		Spec: v1beta2.BrokerClusterSpec{
			ResourceTemplates: []v1beta2.ResourceTemplate{
				{
					// match all
					Labels: map[string]string{"myKey": "myValue-$(CR_NAME)"},
				},
				{
					// match Acceptor Services with Ordinals
					Selector: &v1beta2.ResourceSelector{
						Kind: &kindMatch,
						Name: &matchOrdinalServices,
					},
					Labels: map[string]string{"myKey-$(CR_NAME)": "myValue-$(BROKER_ORDINAL)"},
				},
				{
					// match Ingress
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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.trackDesired(newSS)

	err := routev1.AddToScheme(scheme.Scheme)
	assert.NoError(t, err)

	fakeClient := fake.NewClientBuilder().Build()
	reconciler.ProcessAcceptorsAndConnectors(cr, *namer,
		fakeClient, nil, newSS)

	err = reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var secretFound = false
	var serviceFound = false
	var ssFound = false
	for _, resource := range common.ToResourceList(reconciler.requestedResources) {
		if ss, ok := resource.(*appsv1.StatefulSet); ok {

			v, ok := ss.Labels["myKey"]
			assert.True(t, ok)
			assert.Equal(t, "myValue-cr", v)
			ssFound = true
		}

		if secret, ok := resource.(*v1.Secret); ok {
			assert.True(t, len(secret.Labels) >= 1)
			assert.Equal(t, secret.Labels["myKey"], "myValue-cr", resource.GetName())
			secretFound = true
		}

		if service, ok := resource.(*v1.Service); ok {
			assert.True(t, len(service.Labels) >= 1)
			assert.Equal(t, service.Labels["myKey"], "myValue-cr", resource.GetName())

			if strings.Contains(service.GetName(), "-0-") {
				assert.Equal(t, service.Labels["myKey-cr"], "myValue-0", resource.GetName())
				serviceFound = true
			}
		}

		if ingress, ok := resource.(*netv1.Ingress); ok {
			assert.True(t, len(ingress.Annotations) >= 1)
			assert.Equal(t, ingress.Annotations["myIngressKey-cr"], "myValue-0", resource.GetName())
		}

	}
	assert.True(t, ssFound)
	assert.True(t, secretFound)
	assert.True(t, serviceFound)
}

func TestProcess_TemplateCustomAttributeIngress(t *testing.T) {

	var matchGvForIngress string = "networking.k8s.io/v1"
	var ingressClassVal = "SomeClass"
	cr := &v1beta2.BrokerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cr"},
		Spec: v1beta2.BrokerClusterSpec{
			ResourceTemplates: []v1beta2.ResourceTemplate{
				{
					// match Ingress
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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.trackDesired(newSS)

	fakeClient := fake.NewClientBuilder().Build()
	reconciler.ProcessAcceptorsAndConnectors(cr, *namer,
		fakeClient, nil, newSS)

	err := reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var ingressOk = false
	for _, resource := range common.ToResourceList(reconciler.requestedResources) {

		if ingress, ok := resource.(*netv1.Ingress); ok {
			assert.True(t, len(ingress.Annotations) >= 1)
			assert.Equal(t, ingress.Annotations["myIngressKey-cr"], "myValue-0", resource.GetName())
			assert.NotNil(t, ingress.Spec.IngressClassName)
			assert.Equal(t, *ingress.Spec.IngressClassName, ingressClassVal)
			ingressOk = true
		}
	}
	assert.True(t, ingressOk)
}

func TestProcess_TemplateCustomAttributeMisSpellingIngress(t *testing.T) {

	var matchGvForIngress string = "networking.k8s.io/v1"
	var ingressClassVal = "SomeClass"
	cr := &v1beta2.BrokerCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cr"},
		Spec: v1beta2.BrokerClusterSpec{
			ResourceTemplates: []v1beta2.ResourceTemplate{
				{
					// match Ingress
					Selector: &v1beta2.ResourceSelector{
						APIGroup: &matchGvForIngress,
					},
					Annotations: map[string]string{"myIngressKey-$(CR_NAME)": "myValue-$(BROKER_ORDINAL)"},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{Object: map[string]interface{}{
						"spec": map[string]interface{}{
							"ingressClazzName": ingressClassVal, // wrong attribute
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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)
	newSS, err := reconciler.ProcessStatefulSet(cr, *namer, nil)
	assert.NoError(t, err)

	fakeClient := fake.NewClientBuilder().Build()
	reconciler.ProcessAcceptorsAndConnectors(cr, *namer,
		fakeClient, nil, newSS)

	err = reconciler.ProcessResources(cr, fakeClient, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Clazz")
}

func TestProcess_TemplateCustomAttributeContainerSecurityContext(t *testing.T) {
	testTemplateCustomAttributeContainerSecurityContext(t, false)
}

func TestProcess_TemplateCustomAttributeContainerSecurityContextWithCRNameVar(t *testing.T) {
	testTemplateCustomAttributeContainerSecurityContext(t, true)
}

func testTemplateCustomAttributeContainerSecurityContext(t *testing.T, withCRNameVar bool) {
	var kindMatchSs string = "StatefulSet"
	var containerName string = "cr-container"
	if withCRNameVar {
		containerName = "$(CR_NAME)-container"
	}

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
											"name": containerName, // merge on name key
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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.trackDesired(newSS)

	fakeClient := fake.NewClientBuilder().Build()
	err := reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var runAsRootOk = false
	for _, resource := range common.ToResourceList(reconciler.requestedResources) {

		if ss, ok := resource.(*appsv1.StatefulSet); ok {
			assert.NotNil(t, ss.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot)
			assert.True(t, *ss.Spec.Template.Spec.Containers[0].SecurityContext.RunAsNonRoot)
			assert.NotEqual(t, "", ss.Spec.Template.Spec.Containers[0].Image)
			runAsRootOk = true
		}

	}
	assert.True(t, runAsRootOk)
}

func TestProcess_TemplateCustomAttributePriorityClassName(t *testing.T) {

	var kindMatchSs string = "StatefulSet"

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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	namer := MakeNamers(cr)

	newSS, _ := reconciler.ProcessStatefulSet(cr, *namer, nil)
	reconciler.trackDesired(newSS)

	fakeClient := fake.NewClientBuilder().Build()
	err := reconciler.ProcessResources(cr, fakeClient, nil)
	assert.NoError(t, err)

	var priorityClassNameOk = false
	for _, resource := range common.ToResourceList(reconciler.requestedResources) {

		if ss, ok := resource.(*appsv1.StatefulSet); ok {
			assert.Equal(t, ss.Spec.Template.Spec.PriorityClassName, "high-priority")
			priorityClassNameOk = true
		}

	}
	assert.True(t, priorityClassNameOk)
}

func TestNewPodTemplateSpecForCR_AppendsDebugArgs(t *testing.T) {

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

	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log.WithName("test"), isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedEnv := v1.EnvVar{
		Name:  "DEBUG_ARGS",
		Value: "-Dtest.arg=foo -Djava.security.auth.login.config=/amq/extra/secrets/test-config-jaas-config/login.config",
	}
	assert.Contains(t, newSpec.Spec.Containers[0].Env, expectedEnv)
}

func TestNewPodTemplateSpecForCR_IncludesImagePullSecret(t *testing.T) {

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
	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)
	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedPullSecret := []v1.LocalObjectReference{
		{
			Name: "testPullSecret",
		},
	}
	assert.Equal(t, newSpec.Spec.ImagePullSecrets, expectedPullSecret)
}

func TestNewPodTemplateSpecForCR_IncludesTopologySpreadConstraints(t *testing.T) {
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
	outer := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	reconciler := NewBrokerClusterReconcilerImpl(cr, outer)

	newSpec, err := reconciler.PodTemplateSpecForCR(cr, common.Namers{}, &appsv1.StatefulSet{}, k8sClient)
	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedTopologySpreadConstraints := []v1.TopologySpreadConstraint{
		{
			MaxSkew:           int32(1),
			TopologyKey:       string("topology.kubernetes.io/zone"),
			WhenUnsatisfiable: v1.ScheduleAnyway,
			LabelSelector:     mySelector,
		},
	}
	assert.Equal(t, newSpec.Spec.TopologySpreadConstraints, expectedTopologySpreadConstraints)
}

func TestNewPodTemplateSpecForCR_IncludesContainerSecurityContext(t *testing.T) {
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

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)
	expectedSecurityContext := &v1.SecurityContext{RunAsNonRoot: pointer.To(false)}

	assert.Equal(t, newSpec.Spec.Containers[0].SecurityContext, expectedSecurityContext)
	assert.Equal(t, newSpec.Spec.InitContainers[0].SecurityContext, expectedSecurityContext)
}

func TestNewPodTemplateSpecForCR_IncludesExtraVolumes(t *testing.T) {
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

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)

	// Verify main container has the extra volume mount
	mainContainerHasMount := false
	for _, mount := range newSpec.Spec.Containers[0].VolumeMounts {
		if mount.Name == "my-extra-volume" {
			mainContainerHasMount = true
			assert.Equal(t, "/amq/extra/volumes/my-extra-volume", mount.MountPath)
			break
		}
	}
	assert.True(t, mainContainerHasMount, "Main container should have the extra volume mount")

	// Verify init container has the extra volume mount
	assert.NotEmpty(t, newSpec.Spec.InitContainers, "Should have init containers")
	initContainerHasMount := false
	for _, mount := range newSpec.Spec.InitContainers[0].VolumeMounts {
		if mount.Name == "my-extra-volume" {
			initContainerHasMount = true
			assert.Equal(t, "/amq/extra/volumes/my-extra-volume", mount.MountPath)
			break
		}
	}
	assert.True(t, initContainerHasMount, "Init container should have the extra volume mount")
}

func TestNewPodTemplateSpecForCR_IncludesExtraVolumesWithCustomMount(t *testing.T) {
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

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)

	// Verify main container has the extra volume mount with custom path
	mainContainerHasMount := false
	for _, mount := range newSpec.Spec.Containers[0].VolumeMounts {
		if mount.Name == "my-extra-volume" {
			mainContainerHasMount = true
			assert.Equal(t, "/custom/path", mount.MountPath)
			break
		}
	}
	assert.True(t, mainContainerHasMount, "Main container should have the extra volume mount")

	// Verify init container has the extra volume mount with custom path
	assert.NotEmpty(t, newSpec.Spec.InitContainers, "Should have init containers")
	initContainerHasMount := false
	for _, mount := range newSpec.Spec.InitContainers[0].VolumeMounts {
		if mount.Name == "my-extra-volume" {
			initContainerHasMount = true
			assert.Equal(t, "/custom/path", mount.MountPath)
			break
		}
	}
	assert.True(t, initContainerHasMount, "Init container should have the extra volume mount with custom path")
}

func TestNewPodTemplateSpecForCR_IncludesExtraVolumeClaimTemplates(t *testing.T) {
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

	assert.NoError(t, err)
	assert.NotNil(t, newSpec)

	// Verify main container has the extra volume claim mount
	mainContainerHasMount := false
	for _, mount := range newSpec.Spec.Containers[0].VolumeMounts {
		if mount.Name == "my-pvc" {
			mainContainerHasMount = true
			assert.Equal(t, "/opt/my-pvc/data", mount.MountPath)
			break
		}
	}
	assert.True(t, mainContainerHasMount, "Main container should have the extra PVC mount")

	// Verify init container has the extra volume claim mount
	assert.NotEmpty(t, newSpec.Spec.InitContainers, "Should have init containers")
	initContainerHasMount := false
	for _, mount := range newSpec.Spec.InitContainers[0].VolumeMounts {
		if mount.Name == "my-pvc" {
			initContainerHasMount = true
			assert.Equal(t, "/opt/my-pvc/data", mount.MountPath)
			break
		}
	}
	assert.True(t, initContainerHasMount, "Init container should have the extra PVC mount")
}

func TestLoginConfigSyntaxCheck(t *testing.T) {
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
		assert.True(t, MatchBytesAgainsLoginConfigRegexp(v), "for key "+k)
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
		assert.False(t, MatchBytesAgainsLoginConfigRegexp(v), "for key "+k)
	}

}

func TestStatusMarshall(t *testing.T) {

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
	assert.Nil(t, err)
	assert.True(t, strings.Contains(string(v), ":false"))

}

func TestGetBrokerHost(t *testing.T) {
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
	assert.Equal(t, "test-test-ns-my-acceptor-0-ing.my-domain.com", ingressHost)

	ingressHost = formatTemplatedString(&cr, specIngressHost, "1", "my-connector", "rte")
	assert.Equal(t, "test-test-ns-my-connector-1-rte.my-domain.com", ingressHost)

	ingressHost = formatTemplatedString(&cr, specIngressHost, "2", "my-console", "abc")
	assert.Equal(t, "test-test-ns-my-console-2-abc.my-domain.com", ingressHost)
}

func TestFormatTemplatedStringWithInvalidVariables(t *testing.T) {
	cr := v1beta2.BrokerCluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-ns",
			Name:      "test",
		},
		Spec: v1beta2.BrokerClusterSpec{
			IngressDomain: "my-domain.com",
		},
	}

	assert.Equal(t, "test-$(UNKNOWN_VAR)", formatTemplatedString(&cr, "test-$(UNKNOWN_VAR)", "", "", ""))
	assert.Equal(t, "prefix-test-$(INVALID)-suffix", formatTemplatedString(&cr, "prefix-$(CR_NAME)-$(INVALID)-suffix", "0", "", ""))
}

func TestFormatTemplatedObject(t *testing.T) {
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
	testFormattedObject(t, formattedObject, "test-test-ns-test-name-a-0-test-type-A.my-domain.com")

	formattedObject = formatTemplatedObject(&cr, templatedObject, "1", "test-name-b", "test-type-B").(map[string]interface{})
	testFormattedObject(t, formattedObject, "test-test-ns-test-name-b-1-test-type-B.my-domain.com")
}

func testFormattedObject(t *testing.T, formattedObject map[string]interface{}, expectedString string) {
	assert.Equal(t, "TEST", formattedObject["plain-string"])
	assert.Equal(t, 1, formattedObject["plain-int"])
	assert.Equal(t, expectedString, formattedObject["test-map"].(map[string]interface{})["test-map-key"])
	assert.Equal(t, "TEST", formattedObject["test-map"].(map[string]interface{})["nested-plain-string"])
	assert.Equal(t, 1, formattedObject["test-map"].(map[string]interface{})["nested-plain-int"])
	assert.Equal(t, expectedString, formattedObject["test-map"].(map[string]interface{})["nested-test-array"].([]interface{})[0])
	assert.Equal(t, expectedString, formattedObject["test-array"].([]interface{})[0])
	assert.Equal(t, expectedString, formattedObject["test-array"].([]interface{})[1].(map[string]interface{})["nested-test-map-key"])
	assert.Equal(t, expectedString, formattedObject["test-string"])

}

func TestParseBrokerPropertyWithOrdinal(t *testing.T) {
	var matches []string

	matches = ParseBrokerPropertyWithOrdinal("broker-0.maxDiskUsage")
	assert.Equal(t, 3, len(matches))
	assert.Equal(t, "broker-0.maxDiskUsage", matches[0])
	assert.Equal(t, "broker-0", matches[1])
	assert.Equal(t, "maxDiskUsage", matches[2])

	matches = ParseBrokerPropertyWithOrdinal("broker-999.maxDiskUsage=97")
	assert.Equal(t, 3, len(matches))
	assert.Equal(t, "broker-999.maxDiskUsage=97", matches[0])
	assert.Equal(t, "broker-999", matches[1])
	assert.Equal(t, "maxDiskUsage=97", matches[2])

	matches = ParseBrokerPropertyWithOrdinal("maxDiskUsage=97")
	assert.Equal(t, 0, len(matches))

	matches = ParseBrokerPropertyWithOrdinal("a.broker-0.maxDiskUsage")
	assert.Equal(t, 0, len(matches))

	matches = ParseBrokerPropertyWithOrdinal("broker-0-maxDiskUsage")
	assert.Equal(t, 0, len(matches))

	matches = ParseBrokerPropertyWithOrdinal("broker-a.maxDiskUsage")
	assert.Equal(t, 0, len(matches))
}

func TestBrokerPropertiesData(t *testing.T) {

	data := BrokerPropertiesData([]string{
		"maxDiskUsage=97",
		"minDiskFree=5",
	})

	assert.Equal(t, 1, len(data))

	assert.True(t, strings.Contains(string(data[BrokerPropertiesName]), "maxDiskUsage=97"))
	assert.True(t, strings.Contains(string(data[BrokerPropertiesName]), "minDiskFree=5"))
}

func TestBrokerPropertiesDataWithOrdinal(t *testing.T) {

	data := BrokerPropertiesData([]string{
		"broker-0.maxDiskUsage=98",
		"broker-0.minDiskFree=6",
		"broker-999.maxDiskUsage=99",
		"broker-999.minDiskFree=7",
	})

	assert.Equal(t, 3, len(data))

	assert.False(t, strings.Contains(string(string(data[BrokerPropertiesName])), "maxDiskUsage"))
	assert.False(t, strings.Contains(string(data[BrokerPropertiesName]), "minDiskFree"))

	broker0BrokerPropertiesName := "broker-0" + OrdinalPrefixSep + BrokerPropertiesName
	assert.True(t, strings.Contains(string(data[broker0BrokerPropertiesName]), "maxDiskUsage=98"))
	assert.True(t, strings.Contains(string(data[broker0BrokerPropertiesName]), "minDiskFree=6"))

	broker999BrokerPropertiesName := "broker-999" + OrdinalPrefixSep + BrokerPropertiesName
	assert.True(t, strings.Contains(string(data[broker999BrokerPropertiesName]), "maxDiskUsage=99"))
	assert.True(t, strings.Contains(string(data[broker999BrokerPropertiesName]), "minDiskFree=7"))
}

func TestBrokerPropertiesDataWithAndWithoutOrdinal(t *testing.T) {

	data := BrokerPropertiesData([]string{
		"maxDiskUsage=97",
		"minDiskFree=5",
		"broker-0.maxDiskUsage=98",
		"broker-0.minDiskFree=6",
		"broker-999.maxDiskUsage=99",
		"broker-999.minDiskFree=7",
	})

	assert.Equal(t, 3, len(data))

	assert.True(t, strings.Contains(string(data[BrokerPropertiesName]), "maxDiskUsage=97"))
	assert.True(t, strings.Contains(string(data[BrokerPropertiesName]), "minDiskFree=5"))

	broker0BrokerPropertiesName := "broker-0" + OrdinalPrefixSep + BrokerPropertiesName
	assert.True(t, strings.Contains(string(data[broker0BrokerPropertiesName]), "maxDiskUsage=98"))
	assert.True(t, strings.Contains(string(data[broker0BrokerPropertiesName]), "minDiskFree=6"))

	broker999BrokerPropertiesName := "broker-999" + OrdinalPrefixSep + BrokerPropertiesName
	assert.True(t, strings.Contains(string(data[broker999BrokerPropertiesName]), "maxDiskUsage=99"))
	assert.True(t, strings.Contains(string(data[broker999BrokerPropertiesName]), "minDiskFree=7"))
}

func TestDuplicateKeyIn(t *testing.T) {

	data := []byte("aa\\=a=VAL\naa\\=b=VAL")

	kv := KeyValuePairs(data)

	assert.Equal(t, len(kv), 2)
	assert.True(t, strings.HasPrefix(kv[0], "aa"))
	assert.True(t, strings.HasPrefix(kv[1], "aa"))

	assert.Equal(t, "", DuplicateKeyIn(kv))

}

func TestEnsureOwnerReferenceAPIVersion_NoOwnerReferences(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	existing := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "test-secret",
			OwnerReferences: []metav1.OwnerReference{},
		},
	}

	candidate := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

	assert.True(t, result, "should return true when no owner references exist")
	assert.Empty(t, candidate.GetOwnerReferences(), "candidate owner references should not be modified")
}

func TestEnsureOwnerReferenceAPIVersion_MatchingAPIVersion(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	existing := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "broker.amq.io/v1beta1",
					Kind:       "ActiveMQArtemis",
					Name:       "test-broker",
					UID:        "test-uid",
				},
			},
		},
	}

	candidate := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

	assert.True(t, result, "should return true when API versions match")
	assert.Empty(t, candidate.GetOwnerReferences(), "candidate owner references should not be modified when versions match")
}

func TestEnsureOwnerReferenceAPIVersion_DifferentAPIVersion(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	existing := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "broker.amq.io/v1alpha1",
					Kind:       "ActiveMQArtemis",
					Name:       "test-broker",
					UID:        "test-uid",
				},
			},
		},
	}

	candidate := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

	assert.False(t, result, "should return false when API versions differ")
	assert.Len(t, candidate.GetOwnerReferences(), 1, "candidate should have owner references set")
	assert.Equal(t, "broker.amq.io/v1beta1", candidate.GetOwnerReferences()[0].APIVersion, "candidate should have updated API version")
}

func TestEnsureOwnerReferenceAPIVersion_MultipleOwnerReferences(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	existing := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "other-owner",
					UID:        "other-uid",
				},
				{
					APIVersion: "broker.amq.io/v1alpha1",
					Kind:       "ActiveMQArtemis",
					Name:       "test-broker",
					UID:        "test-uid",
				},
			},
		},
	}

	candidate := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

	assert.False(t, result, "should return false when ActiveMQArtemis owner reference API version differs")
	assert.Len(t, candidate.GetOwnerReferences(), 2, "candidate should have both owner references")
	assert.Equal(t, "apps/v1", candidate.GetOwnerReferences()[0].APIVersion, "first owner reference should remain unchanged")
	assert.Equal(t, "broker.amq.io/v1beta1", candidate.GetOwnerReferences()[1].APIVersion, "ActiveMQArtemis owner reference should be updated")
}

func TestEnsureOwnerReferenceAPIVersion_DifferentBrokerName(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	existing := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "broker.amq.io/v1alpha1",
					Kind:       "ActiveMQArtemis",
					Name:       "different-broker",
					UID:        "test-uid",
				},
			},
		},
	}

	candidate := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-secret",
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

	assert.True(t, result, "should return true when owner reference is for a different broker")
	assert.Empty(t, candidate.GetOwnerReferences(), "candidate owner references should not be modified")
}

func TestCompareSecret_WithAPIVersionUpdate(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	deployed := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "broker.amq.io/v1alpha1",
					Kind:       "ActiveMQArtemis",
					Name:       "test-broker",
					UID:        "test-uid",
				},
			},
		},
		Data: map[string][]byte{
			"key": []byte("value"),
		},
	}

	requested := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"key": []byte("value"),
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.CompareSecret(deployed, requested)

	assert.False(t, result, "should return false when owner reference API version needs update")
	assert.Len(t, requested.GetOwnerReferences(), 1, "requested should have updated owner references")
	assert.Equal(t, "broker.amq.io/v1beta1", requested.GetOwnerReferences()[0].APIVersion, "API version should be updated")
}

func TestCompareConfigMap_WithAPIVersionUpdate(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	deployed := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "test-ns",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "broker.amq.io/v1alpha1",
					Kind:       "ActiveMQArtemis",
					Name:       "test-broker",
					UID:        "test-uid",
				},
			},
		},
	}

	requested := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config",
			Namespace: "test-ns",
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.CompareConfigMap(deployed, requested)

	assert.False(t, result, "should return false when owner reference API version needs update")
	assert.Len(t, requested.GetOwnerReferences(), 1, "requested should have updated owner references")
	assert.Equal(t, "broker.amq.io/v1beta1", requested.GetOwnerReferences()[0].APIVersion, "API version should be updated")
}

func TestCompareMetaAndSpec_WithAPIVersionUpdate(t *testing.T) {
	cr := &v1beta2.BrokerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "broker.amq.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "test-ns",
		},
	}

	deployed := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ss",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "broker.amq.io/v1alpha1",
					Kind:       "ActiveMQArtemis",
					Name:       "test-broker",
					UID:        "test-uid",
				},
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: pointer.To(int32(1)),
		},
	}

	requested := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ss",
			Namespace: "test-ns",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: pointer.To(int32(1)),
		},
	}

	r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerClusterReconcilerImpl(cr, r)

	result := ri.CompareMetaAndSpec(deployed, requested)

	assert.False(t, result, "should return false when owner reference API version needs update")
	assert.Len(t, requested.GetOwnerReferences(), 1, "requested should have updated owner references")
	assert.Equal(t, "broker.amq.io/v1beta1", requested.GetOwnerReferences()[0].APIVersion, "API version should be updated")
}
