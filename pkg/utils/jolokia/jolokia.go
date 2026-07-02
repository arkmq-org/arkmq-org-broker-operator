package jolokia

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const JOLOKIA_AGENT_PORT = "8778"

type IData interface {
	Print()
}

type ResponseData struct {
	Status    int
	Value     string
	ErrorType string
	Error     string
}

type ReadRequest struct {
	MBean     string `json:"mbean"`
	Attribute string `json:"attribute"`
	Type      string `json:"type"`
}

type JolokiaError struct {
	HttpCode int
	Message  string
}

func (j *JolokiaError) Error() string {
	return fmt.Sprintf("HTTP STATUS %v. Message: %v", j.HttpCode, j.Message)
}

type IJolokia interface {
	Read(path string) (*ResponseData, error)
	Exec(path, postJsonString string) (*ResponseData, error)
	ExecWithClient(httpClient *http.Client, path, postJsonString string) (*ResponseData, error)
	GetProtocol() string
	GetClientWithTimeout(timeout time.Duration) *http.Client
}

type Jolokia struct {
	ip         string
	port       string
	jolokiaURL string
	user       string
	password   string
	protocol   string
	restricted bool
	client     rtclient.Client
}

func GetRestrictedJolokia(client rtclient.Client, _ip string, _port string, _path string) *Jolokia {
	j := Jolokia{
		ip:         _ip,
		port:       _port,
		jolokiaURL: _ip + ":" + _port + _path,
		protocol:   "https",
		restricted: true,
		client:     client,
	}
	return &j
}

func GetJolokia(_client rtclient.Client, _ip string, _port string, _path string, _user string, _password string, _protocol string) *Jolokia {

	j := Jolokia{
		ip:         _ip,
		port:       _port,
		jolokiaURL: _ip + ":" + _port + _path,
		user:       _user,
		password:   _password,
		protocol:   _protocol,
		client:     _client,
	}
	if j.user == "" {
		j.user = "admin"
	}
	if j.password == "" {
		j.password = "admin"
	} else {
		//encode password in case it has special chars
		j.password = url.QueryEscape(j.password)
	}

	return &j
}

func (j *Jolokia) GetProtocol() string {
	return j.protocol
}

func (j *Jolokia) getClient() *http.Client {
	// A timeout less than 3 seconds may cause connection issues when
	// the server requires to change the chiper.
	return j.GetClientWithTimeout(time.Second * 3)
}

func (j *Jolokia) GetClientWithTimeout(timeout time.Duration) *http.Client {
	httpClient := http.Client{
		Transport: http.DefaultTransport,
		Timeout:   timeout,
	}

	if j.protocol == "https" {
		httpClientTransport := httpClient.Transport.(*http.Transport)
		httpClientTransport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         j.ip,
		}
		if common.OperatorHasCertAndTrustBundle(j.client) {
			httpClientTransport.TLSClientConfig.InsecureSkipVerify = false
			httpClientTransport.TLSClientConfig.GetClientCertificate =
				func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
					return common.GetOperatorClientCertificate(j.client, cri)
				}
		}
		if rootCas, err := common.GetRootCAs(j.client); err == nil {
			httpClientTransport.TLSClientConfig.RootCAs = rootCas
		}
	}

	return &httpClient
}

func (j *Jolokia) Read(_path string) (*ResponseData, error) {

	url := j.protocol + "://" + j.user + ":" + j.password + "@" + j.jolokiaURL + "/read/" + _path

	jolokiaClient := j.getClient()

	var respError error = nil
	var jdata *ResponseData = nil

	for {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			break
		}
		req.Header.Set("User-Agent", "arkmq-org-broker-management")
		res, err := jolokiaClient.Do(req)

		if err != nil {
			respError = err
			break
		}
		defer res.Body.Close()

		if !isResponseSuccessful(res.StatusCode) {
			return nil, &JolokiaError{
				HttpCode: res.StatusCode,
				Message:  "error: " + res.Status,
			}
		}

		//decoding
		result, _, err := decodeResponseData(res)
		if err != nil {
			respError = err
			return result, err
		}

		jdata = result

		//before decoding the body, we need to check the http code
		err = CheckResponse(res, result)

		if err != nil {
			respError = err
		}

		break
	}

	return jdata, respError
}

func (j *Jolokia) Exec(_path string, _postJsonString string) (*ResponseData, error) {
	return j.ExecWithClient(j.getClient(), _path, _postJsonString)
}

func (j *Jolokia) ExecWithClient(jolokiaClient *http.Client, _path string, _postJsonString string) (*ResponseData, error) {

	url := j.protocol + "://" + j.user + ":" + j.password + "@" + j.jolokiaURL + "/exec/" + _path

	var jdata *ResponseData
	var execErr error = nil
	for {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer([]byte(_postJsonString)))
		if err != nil {
			execErr = err
			break
		}

		req.Header.Set("User-Agent", "arkmq-org-broker-management")
		req.Header.Set("Content-Type", "application/json")
		res, err := jolokiaClient.Do(req)

		if err != nil {
			execErr = err
			break
		}

		defer res.Body.Close()

		//decoding
		result, _, err := decodeResponseData(res)
		if err != nil {
			return result, err
		}

		jdata = result

		err = CheckResponse(res, result)
		if err != nil {
			execErr = err
		}

		break
	}

	return jdata, execErr
}

func CheckResponse(resp *http.Response, jdata *ResponseData) error {

	if isResponseSuccessful(resp.StatusCode) {
		//that doesn't mean it's ok, check further
		if isResponseSuccessful(jdata.Status) {
			return nil
		}
		errCode := jdata.Status
		errType := jdata.ErrorType
		errMsg := jdata.Error
		errData := jdata.Value
		internalErr := fmt.Errorf("Error response code %v, type %v, message %v, %v", errCode, errType, errMsg, errData)
		return internalErr
	}
	return &JolokiaError{
		HttpCode: resp.StatusCode,
		Message:  " Error: " + resp.Status,
	}
}

func isResponseSuccessful(httpCode int) bool {
	return httpCode >= 200 && httpCode <= 299
}

func decodeResponseData(resp *http.Response) (*ResponseData, map[string]interface{}, error) {
	result := &ResponseData{}
	rawData := make(map[string]interface{})
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return nil, rawData, err
	}

	//fill in response data
	if v, ok := rawData["error"]; ok {
		if v != nil {
			result.Error = fmt.Sprintf("%v", v)
		}
	}
	if v, ok := rawData["error_type"]; ok {
		if v != nil {
			result.ErrorType = fmt.Sprintf("%v", v)
		}
	}
	if v, ok := rawData["status"]; ok {
		if v != nil {
			result.Status = int(v.(float64))
		}
	}
	if v, ok := rawData["value"]; ok {
		if v != nil {
			result.Value = fmt.Sprintf("%v", v)
		}
	}

	return result, rawData, nil

}
