package lsrcrs

import (
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StoredCR struct {
	Owner        v1.Object
	Name         string
	Namespace    string
	CRType       string
	CR           string
	Data         string
	Checksum     string
	UpdateClient client.Client
	Scheme       *runtime.Scheme
}

type LastSuccessfulReconciledCR struct {
	CR        string
	Data      string
	Checksum  string
	Timestamp string
}

func StoreLastSuccessfulReconciledCR(owner v1.Object,
	name string, namespace string, crType string, cr string, data string, checksum string,
	labels map[string]string, client client.Client, scheme *runtime.Scheme) error {
	log := ctrl.Log.WithName("lsrcr")

	secretName := "secret-" + crType + "-" + name
	secretNn := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}
	secretData := make(map[string][]byte)
	secretData["CR"] = []byte(cr)
	secretData["Data"] = []byte(data)
	secretData["Checksum"] = []byte(checksum)
	secretData["Timestamp"] = []byte(time.Now().String())
	err := secrets.CreateOrUpdate(owner, secretNn, secretData, labels, client, scheme)
	if err != nil {
		log.Error(err, "failed to save lsrcr", "for cr", name, "secret", secretName, "ns", namespace)
	}
	return err
}

func deleteLastSuccessfulReconciledCR(scr *StoredCR, labels map[string]string) {
	secretName := "secret-" + scr.CRType + "-" + scr.Name
	secretNn := types.NamespacedName{
		Name:      secretName,
		Namespace: scr.Namespace,
	}
	secretData := make(map[string][]byte)
	secretData["CR"] = []byte(scr.CR)
	secretData["Data"] = []byte(scr.Data)
	secretData["Checksum"] = []byte(scr.Checksum)
	secretData["Timestamp"] = []byte(time.Now().String())

	secrets.Delete(secretNn, secretData, labels, scr.UpdateClient)
}

func retrieveLastSuccessfulReconciledCR(scr *StoredCR, labels map[string]string) *LastSuccessfulReconciledCR {
	log := ctrl.Log.WithName("lsrcr").WithValues("cr", *scr)
	var lsrcr *LastSuccessfulReconciledCR = nil
	secretName := "secret-" + scr.CRType + "-" + scr.Name
	secretNn := types.NamespacedName{
		Name:      secretName,
		Namespace: scr.Namespace,
	}
	log.V(2).Info("trying retriving lsrcr", "ns", secretNn, "sec name", secretName, "client", scr.UpdateClient)
	theSecret, err := secrets.RetriveSecret(secretNn, labels, scr.UpdateClient)
	if err != nil {
		if !errors.IsNotFound(err) {
			log.Error(err, "failed to retrieve secret", "secret", secretName, "ns", scr.Namespace)
		}
	} else {
		lsrcr = lsrcrFromSecret(theSecret)
	}

	return lsrcr
}

func lsrcrFromSecret(secret *corev1.Secret) *LastSuccessfulReconciledCR {
	lsrcr := LastSuccessfulReconciledCR{
		CR:        string(secret.Data["CR"]),
		Data:      string(secret.Data["Data"]),
		Checksum:  string(secret.Data["Checksum"]),
		Timestamp: string(secret.Data["Timestamp"]),
	}
	return &lsrcr
}

func DeleteLastSuccessfulReconciledCR(namespacedName types.NamespacedName, crType string, labels map[string]string, client client.Client) *LastSuccessfulReconciledCR {
	scr := StoredCR{
		Name:         namespacedName.Name,
		Namespace:    namespacedName.Namespace,
		CRType:       crType,
		UpdateClient: client,
	}
	lsrcr := retrieveLastSuccessfulReconciledCR(&scr, labels)
	if lsrcr != nil {
		deleteLastSuccessfulReconciledCR(&scr, labels)
	}
	return lsrcr
}

func RetrieveLastSuccessfulReconciledCR(namespacedName types.NamespacedName, crType string, client client.Client, labels map[string]string) *LastSuccessfulReconciledCR {

	scr := StoredCR{
		Name:         namespacedName.Name,
		Namespace:    namespacedName.Namespace,
		CRType:       crType,
		UpdateClient: client,
	}
	return retrieveLastSuccessfulReconciledCR(&scr, labels)
}
