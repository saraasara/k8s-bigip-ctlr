package controller

import (
	"bytes"
	"fmt"
	cisapiv1 "github.com/F5Networks/k8s-bigip-ctlr/v3/config/apis/cis/v1"
	"github.com/F5Networks/k8s-bigip-ctlr/v3/pkg/tokenmanager"
	mockhc "github.com/f5devcentral/mockhttpclient"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	routeapi "github.com/openshift/api/route/v1"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	"net/http"
	"sync"
	"testing"
)

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CR Manager Suite")
}

var configPath = "../../test/configs/"

type (
	mockController struct {
		*Controller
	}

	mockPostManager struct {
		*PostManager
		Responses []int
		RespIndex int
	}

	responceCtx struct {
		tenant string
		status float64
		body   string
	}
)

func newMockController() *mockController {
	tokenManager := tokenmanager.NewTokenManager("https://0.0.0.0", tokenmanager.Credentials{Username: "admin", Password: "admin"}, "admin", false)
	return &mockController{
		Controller: &Controller{
			resourceSelectorConfig: ResourceSelectorConfig{},
			CMTokenManager:         tokenManager,
			RequestHandler: &RequestHandler{
				PostManagers: PostManagers{sync.RWMutex{}, make(map[BigIpKey]*PostManager)},
				reqChan:      make(chan ResourceConfigRequest, 1),
				PostParams:   PostParams{tokenManager: tokenManager},
			},
			bigIpMap:         make(BigIpMap),
			PostParams:       PostParams{},
			clientsets:       &ClientSets{},
			managedResources: ManagedResources{ManageVirtualServer: true, ManageIL: true, ManageEDNS: true, ManageTransportServer: true, ManageTLSProfile: true, ManageSecrets: true},
		},
	}
}

func (m *mockController) shutdown() error {
	return nil
}

func newMockPostManger() *mockPostManager {
	mockPM := &mockPostManager{
		PostManager: &PostManager{
			postChan:            make(chan agentConfig, 1),
			cachedTenantDeclMap: make(map[string]as3Tenant),
			respChan:            make(chan *agentConfig, 1),
		},
		Responses: []int{},
		RespIndex: 0,
	}
	mockPM.AS3PostManager = &AS3PostManager{}
	mockPM.AS3PostManager.firstPost = true
	mockPM.tokenManager = tokenmanager.NewTokenManager(
		"0.0.0.0",
		tokenmanager.Credentials{Username: "admin", Password: "admin"}, "", false)
	return mockPM
}

func getMockHttpClient(responces []responceCtx, method string) (*http.Client, error) {
	var body string
	responseMap := make(mockhc.ResponseConfigMap)
	responseMap[method] = &mockhc.ResponseConfig{}

	for _, resp := range responces {
		if resp.body == "" {
			if resp.status == http.StatusOK {
				body = fmt.Sprintf(`{"results":[{"code":%f,"message":"none", "tenant": "%s"}], "declaration": {"%s": {"Shared": {"class": "application"}}}}`,
					resp.status, resp.tenant, resp.tenant)
			} else {
				body = fmt.Sprintf(`{"results":[{"code":%f,"message":"none", "tenant": "%s"}],"error":{"code":%f}}`,
					resp.status, resp.tenant, resp.status)
			}
		} else {
			body = resp.body
		}

		responseMap[method].Responses = append(responseMap[method].Responses, &http.Response{
			StatusCode: int(resp.status),
			Header:     http.Header{},
			Body:       ioutil.NopCloser(bytes.NewReader([]byte(body))),
		})
	}

	return mockhc.NewMockHTTPClient(responseMap)
}

func (mockPM *mockPostManager) setResponses(responces []responceCtx, method string) {
	client, _ := getMockHttpClient(responces, method)
	mockPM.PostParams.httpClient = client
}

func newMockAgent(userAgent string) *RequestHandler {
	return &RequestHandler{
		PostManagers: PostManagers{sync.RWMutex{}, make(map[BigIpKey]*PostManager)},
		userAgent:    userAgent,
	}
}
func (m *mockController) addEDNS(edns *cisapiv1.ExternalDNS) {
	appInf, _ := m.getNamespacedCommonInformer(edns.ObjectMeta.Namespace)
	appInf.ednsInformer.GetStore().Add(edns)
	if m.resourceQueue != nil {
		m.enqueueExternalDNS(edns)
	}
}

func (m *mockController) deleteEDNS(edns *cisapiv1.ExternalDNS) {
	appInf, _ := m.getNamespacedCommonInformer(edns.ObjectMeta.Namespace)
	appInf.ednsInformer.GetStore().Delete(edns)
	if m.resourceQueue != nil {
		m.enqueueDeletedExternalDNS(edns)
	}
}

func (m *mockController) addRoute(route *routeapi.Route) {
	appInf, _ := m.getNamespacedNativeInformer(route.ObjectMeta.Namespace)
	appInf.routeInformer.GetStore().Add(route)
	if m.resourceQueue != nil {
		m.enqueueRoute(route, Create)
	}
}

func (m *mockController) deleteRoute(route *routeapi.Route) {
	appInf, _ := m.getNamespacedNativeInformer(route.ObjectMeta.Namespace)
	appInf.routeInformer.GetStore().Delete(route)
	if m.resourceQueue != nil {
		m.enqueueDeletedRoute(route)
	}
}

func (m *mockController) updateRoute(route *routeapi.Route) {
	appInf, _ := m.getNamespacedNativeInformer(route.ObjectMeta.Namespace)
	appInf.routeInformer.GetStore().Update(route)
}
func (m *mockController) addService(svc *v1.Service) {
	comInf, _ := m.getNamespacedCommonInformer(svc.ObjectMeta.Namespace)
	comInf.svcInformer.GetStore().Add(svc)

	if m.resourceQueue != nil {
		m.enqueueService(svc, "")
	}
}

func (m *mockController) updateService(svc *v1.Service) {
	comInf, _ := m.getNamespacedCommonInformer(svc.ObjectMeta.Namespace)
	comInf.svcInformer.GetStore().Update(svc)
}

func (m *mockController) deleteService(svc *v1.Service) {
	comInf, _ := m.getNamespacedCommonInformer(svc.ObjectMeta.Namespace)
	comInf.svcInformer.GetStore().Delete(svc)
	if m.resourceQueue != nil {
		m.enqueueDeletedService(svc, "")
	}
}

func (m *mockController) addEndpoints(ep *v1.Endpoints) {
	comInf, _ := m.getNamespacedCommonInformer(ep.ObjectMeta.Namespace)
	comInf.epsInformer.GetStore().Add(ep)

	if m.resourceQueue != nil {
		m.enqueueEndpoints(ep, Create, "")
	}
}

func (m *mockController) updateEndpoints(ep *v1.Endpoints) {
	comInf, _ := m.getNamespacedCommonInformer(ep.ObjectMeta.Namespace)
	comInf.epsInformer.GetStore().Update(ep)
}

func (m *mockController) deleteEndpoints(ep *v1.Endpoints) {
	comInf, _ := m.getNamespacedCommonInformer(ep.ObjectMeta.Namespace)
	comInf.epsInformer.GetStore().Delete(ep)
	if m.resourceQueue != nil {
		m.enqueueEndpoints(ep, Delete, "")
	}
}

func convertSvcPortsToEndpointPorts(svcPorts []v1.ServicePort) []v1.EndpointPort {
	eps := make([]v1.EndpointPort, len(svcPorts))
	for i, v := range svcPorts {
		eps[i].Name = v.Name
		eps[i].Port = v.Port
	}
	return eps
}

func (m *mockController) addVirtualServer(vs *cisapiv1.VirtualServer) {
	cusInf, _ := m.getNamespacedCRInformer(vs.ObjectMeta.Namespace)
	cusInf.vsInformer.GetStore().Add(vs)

	if m.resourceQueue != nil {
		m.enqueueVirtualServer(vs)
	}
}

func (m *mockController) updateVirtualServer(oldVS *cisapiv1.VirtualServer, newVS *cisapiv1.VirtualServer) {
	cusInf, _ := m.getNamespacedCRInformer(oldVS.ObjectMeta.Namespace)
	cusInf.vsInformer.GetStore().Update(newVS)

	if m.resourceQueue != nil {
		m.enqueueUpdatedVirtualServer(oldVS, newVS)
	}
}

func (m *mockController) deleteVirtualServer(vs *cisapiv1.VirtualServer) {
	cusInf, _ := m.getNamespacedCRInformer(vs.ObjectMeta.Namespace)
	cusInf.vsInformer.GetStore().Delete(vs)

	if m.resourceQueue != nil {
		m.enqueueDeletedVirtualServer(vs)
	}
}

func (m *mockController) addTransportServer(vs *cisapiv1.TransportServer) {
	cusInf, _ := m.getNamespacedCRInformer(vs.ObjectMeta.Namespace)
	cusInf.tsInformer.GetStore().Add(vs)

	if m.resourceQueue != nil {
		m.enqueueTransportServer(vs)
	}
}

func (m *mockController) updateTransportServer(oldVS *cisapiv1.TransportServer, newVS *cisapiv1.TransportServer) {
	cusInf, _ := m.getNamespacedCRInformer(oldVS.ObjectMeta.Namespace)
	cusInf.tsInformer.GetStore().Update(newVS)

	if m.resourceQueue != nil {
		m.enqueueUpdatedTransportServer(oldVS, newVS)
	}
}

func (m *mockController) deleteTransportServer(vs *cisapiv1.TransportServer) {
	cusInf, _ := m.getNamespacedCRInformer(vs.ObjectMeta.Namespace)
	cusInf.tsInformer.GetStore().Delete(vs)

	if m.resourceQueue != nil {
		m.enqueueDeletedTransportServer(vs)
	}
}

func (m *mockController) addPolicy(plc *cisapiv1.Policy) {
	cusInf, _ := m.getNamespacedCommonInformer(plc.ObjectMeta.Namespace)
	cusInf.plcInformer.GetStore().Add(plc)

	if m.resourceQueue != nil {
		m.enqueuePolicy(plc, Create)
	}
}

func (m *mockController) deletePolicy(plc *cisapiv1.Policy) {
	cusInf, _ := m.getNamespacedCommonInformer(plc.ObjectMeta.Namespace)
	cusInf.plcInformer.GetStore().Delete(plc)

	if m.resourceQueue != nil {
		m.enqueueDeletedPolicy(plc)
	}
}

func (m *mockController) addTLSProfile(prof *cisapiv1.TLSProfile) {
	cusInf, _ := m.getNamespacedCRInformer(prof.ObjectMeta.Namespace)
	cusInf.tlsInformer.GetStore().Add(prof)

	if m.resourceQueue != nil {
		m.enqueueTLSProfile(prof, Create)
	}
}

func (m *mockController) addSecret(secret *v1.Secret) {
	comInf, _ := m.getNamespacedCommonInformer(secret.ObjectMeta.Namespace)
	comInf.secretsInformer.GetStore().Add(secret)

	if m.resourceQueue != nil {
		m.enqueueSecret(secret, Create)
	}
}

func (m *mockController) addIngressLink(il *cisapiv1.IngressLink) {
	cusInf, _ := m.getNamespacedCRInformer(il.ObjectMeta.Namespace)
	cusInf.ilInformer.GetStore().Add(il)

	if m.resourceQueue != nil {
		m.enqueueIngressLink(il)
	}
}

func (m *mockController) updateIngressLink(oldIL *cisapiv1.IngressLink, newIL *cisapiv1.IngressLink) {
	cusInf, _ := m.getNamespacedCRInformer(oldIL.ObjectMeta.Namespace)
	cusInf.ilInformer.GetStore().Update(newIL)

	if m.resourceQueue != nil {
		m.enqueueUpdatedIngressLink(oldIL, newIL)
	}
}

func (m *mockController) deleteIngressLink(il *cisapiv1.IngressLink) {
	cusInf, _ := m.getNamespacedCRInformer(il.ObjectMeta.Namespace)
	cusInf.ilInformer.GetStore().Delete(il)

	if m.resourceQueue != nil {
		m.enqueueDeletedIngressLink(il)
	}
}

func (m *mockController) addPod(pod *v1.Pod) {
	cusInf, _ := m.getNamespacedCommonInformer(pod.ObjectMeta.Namespace)
	cusInf.podInformer.GetStore().Add(pod)

	if m.resourceQueue != nil {
		m.enqueuePod(pod, "")
	}
}

func (m *mockController) updatePod(pod *v1.Pod) {
	cusInf, _ := m.getNamespacedCommonInformer(pod.ObjectMeta.Namespace)
	cusInf.podInformer.GetStore().Update(pod)

	if m.resourceQueue != nil {
		m.enqueuePod(pod, "")
	}
}

func (m *mockController) deletePod(pod v1.Pod) {
	cusInf, _ := m.getNamespacedCommonInformer(pod.ObjectMeta.Namespace)
	cusInf.podInformer.GetStore().Delete(pod)

	if m.resourceQueue != nil {
		m.enqueueDeletedPod(pod, "")
	}
}

func (m *mockController) addConfigCR(configCR *cisapiv1.DeployConfig) {
	cusInf, _ := m.getNamespacedCommonInformer(configCR.ObjectMeta.Namespace)
	cusInf.configCRInformer.GetStore().Add(configCR)

	if m.resourceQueue != nil {
		m.enqueueConfigCR(configCR, Create)
	}
}

func (m *mockController) updateConfigCR(configCR *cisapiv1.DeployConfig) {
	cusInf, _ := m.getNamespacedCommonInformer(configCR.ObjectMeta.Namespace)
	cusInf.configCRInformer.GetStore().Update(configCR)

	if m.resourceQueue != nil {
		m.enqueueConfigCR(configCR, Update)
	}
}

func (m *mockController) deleteConfigCR(configCR *cisapiv1.DeployConfig) {
	cusInf, _ := m.getNamespacedCommonInformer(configCR.ObjectMeta.Namespace)
	cusInf.configCRInformer.GetStore().Delete(configCR)

	if m.resourceQueue != nil {
		m.enqueueDeletedConfigCR(configCR)
	}
}

func (m *mockController) addNode(node *v1.Node) {
	nodeInf, _ := m.multiClusterNodeInformers[""]
	nodeInf.nodeInformer.GetStore().Add(node)
	if m.resourceQueue != nil {
		m.SetupNodeProcessing("")
	}
}

func (m *mockController) updateNode(node *v1.Node, ns string) {
	nodeInf, _ := m.multiClusterNodeInformers[""]
	nodeInf.nodeInformer.GetStore().Update(node)
	if m.resourceQueue != nil {
		m.SetupNodeProcessing("")
	}
}

func (m *mockController) updateStatusNode(node *v1.Node, ns string) {
	nodeInf, _ := m.multiClusterNodeInformers[""]
	nodeInf.nodeInformer.GetStore().Update(node)
	if m.resourceQueue != nil {
		m.SetupNodeProcessing("")
	}
}

//func (mockCtlr *mockController) getOrderedRoutes(resourceType, namespace string) []interface{} {
//	return mockCtlr.mockResources[namespace+"/"+resourceType]
//}
//
//func (mockCtlr *mockController) getServicePort(rt *routeapi.Route) (error, int32) {
//	if isSecureRoute(rt) {
//		return nil, 443
//	}
//	return nil, 80
//}
