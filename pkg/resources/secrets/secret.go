package secrets

import (
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var CredentialsNameBuilder namer.NamerData
var ConsoleNameBuilder namer.NamerData
var NettyNameBuilder namer.NamerData

func MakeSecret(namespacedName types.NamespacedName, data map[string][]byte, labels map[string]string) corev1.Secret {

	secretDefinition := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    labels,
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
		Data: data,
	}

	return secretDefinition
}

func NewSecret(namespacedName types.NamespacedName, stringData map[string][]byte, labels map[string]string) *corev1.Secret {

	secretDefinition := MakeSecret(namespacedName, stringData, labels)

	return &secretDefinition
}

func CreateOrUpdate(owner metav1.Object, namespacedName types.NamespacedName, stringDataMap map[string][]byte, labels map[string]string, client client.Client, scheme *runtime.Scheme) error {
	log := ctrl.Log.WithName("util_secrets")
	var err error = nil
	secretDefinition := NewSecret(namespacedName, stringDataMap, labels)

	if err = resources.Retrieve(namespacedName, client, secretDefinition); err != nil {
		if errors.IsNotFound(err) {
			err = resources.Create(owner, client, scheme, secretDefinition)
			if err != nil {
				log.Error(err, "failed to create secret", "secret", namespacedName)
			}
		} else {
			log.Error(err, "Error retrieving secret", "secret", namespacedName.Name)
		}
	} else {
		//Update
		secretDefinition = NewSecret(namespacedName, stringDataMap, labels)
		if err = resources.Update(client, secretDefinition); err != nil {
			log.Error(err, "Failed to update secret", "secret", namespacedName.Name)
		}
	}

	return err
}

func Create(owner metav1.Object, namespacedName types.NamespacedName, stringDataMap map[string][]byte, labels map[string]string, client client.Client, scheme *runtime.Scheme) *corev1.Secret {
	log := ctrl.Log.WithName("util_secrets")

	var err error = nil
	secretDefinition := NewSecret(namespacedName, stringDataMap, labels)

	if err = resources.Retrieve(namespacedName, client, secretDefinition); err != nil {
		if errors.IsNotFound(err) {
			err = resources.Create(owner, client, scheme, secretDefinition)
			if err != nil {
				log.Error(err, "failed to create secret", "secret", namespacedName)
			}
		}
	}

	return secretDefinition
}

func Delete(namespacedName types.NamespacedName, stringDataMap map[string][]byte, labels map[string]string, client client.Client) {
	secretDefinition := NewSecret(namespacedName, stringDataMap, labels)
	resources.Delete(client, secretDefinition)
}

func RetriveSecret(namespacedName types.NamespacedName, labels map[string]string, client client.Client) (*corev1.Secret, error) {
	stringData := make(map[string][]byte)
	secretDefinition := MakeSecret(namespacedName, stringData, labels)
	if err := resources.Retrieve(namespacedName, client, &secretDefinition); err != nil {
		return nil, err
	}
	return &secretDefinition, nil
}

func GetValueFromSecret(namespace string,
	secretName string, key string, labels map[string]string, client client.Client, scheme *runtime.Scheme, owner metav1.Object) *string {
	log := ctrl.Log.WithName("util_secrets")

	//check if the secret exists.
	namespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}
	// Attempt to retrieve the secret
	dataMap := make(map[string][]byte)

	secretDefinition := NewSecret(namespacedName, dataMap, labels)

	if err := resources.Retrieve(namespacedName, client, secretDefinition); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
	} else {
		log.V(2).Info("Found secret " + secretName)
		if elem, ok := secretDefinition.Data[key]; ok {
			//the value exists
			value := string(elem)
			return &value
		}
	}
	return nil
}
