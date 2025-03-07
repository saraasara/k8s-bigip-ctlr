package controller

import (
	ficV1 "github.com/F5Networks/f5-ipam-controller/pkg/ipamapis/apis/fic/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"sync"
	"time"

	cisapiv1 "github.com/F5Networks/k8s-bigip-ctlr/v3/config/apis/cis/v1"
	log "github.com/F5Networks/k8s-bigip-ctlr/v3/pkg/vlogger"
)

func (ctlr *Controller) enqueueReq(config BigIpResourceConfig, bigIpKey BigIpKey) requestMeta {
	rm := requestMeta{
		partitionMap: make(map[string]map[string]string, len(config.ltmConfig)),
	}
	ctlr.requestMap.Lock()
	if reqId, found := ctlr.requestMap.requestMap[bigIpKey]; found {
		rm.id = reqId.id + 1
	} else {
		rm.id = 1
	}
	for partition, partitionConfig := range config.ltmConfig {
		rm.partitionMap[partition] = make(map[string]string)
		for _, cfg := range partitionConfig.ResourceMap {
			for key, val := range cfg.MetaData.baseResources {
				rm.partitionMap[partition][key] = val
			}
		}
	}
	ctlr.requestMap.requestMap[bigIpKey] = rm
	ctlr.requestMap.Unlock()
	return rm
}

func (ctlr *Controller) responseHandler(respChan chan *agentConfig) {
	// todo: update only when there is a change(success to fail or vice versa) in tenant status
	ctlr.requestMap = &requestMap{sync.Mutex{}, make(map[BigIpKey]requestMeta)}
	//TODO: Need to get bigipLabel from rspchan
	bigipLabel := BigIPLabel
	bigipConfig := ctlr.getBIGIPConfig(bigipLabel)
	for config := range respChan {
		ctlr.requestMap.Lock()
		latestRequestMeta, _ := ctlr.requestMap.requestMap[config.BigIpKey]
		ctlr.requestMap.Unlock()
		if len(config.as3Config.failedTenants) > 0 && latestRequestMeta.id == config.id {
			// if the current request id is same as the failed tenant request id, then retry the failed tenants
			ctlr.RequestHandler.PostManagers.RLock()
			pm := ctlr.RequestHandler.PostManagers.PostManagerMap[config.BigIpKey]
			// Delay the retry of failed tenants
			<-time.After(timeoutMedium)
			pm.postChan <- *config
			ctlr.RequestHandler.PostManagers.RUnlock()
		}
		if latestRequestMeta.id >= config.id && len(config.as3Config.failedTenants) == 0 {
			// Handle the network routes after successful post of tenants
			ctlr.processStaticRouteUpdate()
			// if the current request id is less than or equal to the latest request id, then udpate the status for current request
			for partition, meta := range config.reqMeta.partitionMap {
				// Check if it's a priority tenant and not in failedTenants map, if so then update the priority back to zero
				// Priority tenant doesn't have any meta
				if _, found := config.as3Config.failedTenants[partition]; !found && len(meta) == 0 {
					// updating the tenant priority back to zero if it's not in failed tenants
					ctlr.resources.updatePartitionPriority(partition, 0, bigipConfig)
					continue
				}
				for rscKey, kind := range meta {
					ctlr.removeUnusedIPAMEntries(kind)
					ns := strings.Split(rscKey, "/")[0]
					switch kind {
					//case VirtualServer:
					//	// update status
					//	crInf, ok := ctlr.getNamespacedCRInformer(ns)
					//	if !ok {
					//		log.Debugf("VirtualServer Informer not found for namespace: %v", ns)
					//		continue
					//	}
					//	obj, exist, err := crInf.vsInformer.GetIndexer().GetByKey(rscKey)
					//	if err != nil {
					//		log.Debugf("Could not fetch VirtualServer: %v: %v", rscKey, err)
					//		continue
					//	}
					//	if !exist {
					//		log.Debugf("VirtualServer Not Found: %v", rscKey)
					//		continue
					//	}
					//	virtual := obj.(*cisapiv1.VirtualServer)
					//	if virtual.Namespace+"/"+virtual.Name == rscKey {
					//		if _, found := config.as3Config.failedTenants[partition]; !found {
					//			// update the status for virtual server as tenant posting is success
					//			ctlr.updateVirtualServerStatus(virtual, virtual.Status.VSAddress, "Ok")
					//			// Update Corresponding Service Status of Type LB
					//			for _, pool := range virtual.Spec.Pools {
					//				var svcNamespace string
					//				if pool.ServiceNamespace != "" {
					//					svcNamespace = pool.ServiceNamespace
					//				} else {
					//					svcNamespace = virtual.Namespace
					//				}
					//				svc := ctlr.GetService(svcNamespace, pool.Service)
					//				if svc != nil && svc.Spec.Type == v1.ServiceTypeLoadBalancer {
					//					ctlr.setLBServiceIngressStatus(svc, virtual.Status.VSAddress)
					//				}
					//			}
					//		}
					//	}

					case TransportServer:
						// update status
						crInf, ok := ctlr.getNamespacedCRInformer(ns)
						if !ok {
							log.Debugf("TransportServer Informer not found for namespace: %v", ns)
							continue
						}
						obj, exist, err := crInf.tsInformer.GetIndexer().GetByKey(rscKey)
						if err != nil {
							log.Debugf("Could not fetch TransportServer: %v: %v", rscKey, err)
							continue
						}
						if !exist {
							log.Debugf("TransportServer Not Found: %v", rscKey)
							continue
						}
						virtual := obj.(*cisapiv1.TransportServer)
						if virtual.Namespace+"/"+virtual.Name == rscKey {
							if _, found := config.as3Config.failedTenants[partition]; !found {
								// update the status for transport server as tenant posting is success
								ctlr.updateTransportServerStatus(virtual, virtual.Status.VSAddress, "Ok")
								//// Update Corresponding Service Status of Type LB
								//var svcNamespace string
								//if virtual.Spec.Pool.ServiceNamespace != "" {
								//	svcNamespace = virtual.Spec.Pool.ServiceNamespace
								//} else {
								//	svcNamespace = virtual.Namespace
								//}
								//svc := ctlr.GetService(svcNamespace, virtual.Spec.Pool.Service)
								//if svc != nil && svc.Spec.Type == v1.ServiceTypeLoadBalancer {
								//	ctlr.setLBServiceIngressStatus(svc, virtual.Status.VSAddress)
								//}
							}
						}
						//case Route:
						//	if _, found := config.as3Config.failedTenants[partition]; found {
						//		// TODO : distinguish between a 503 and an actual failure
						//		go ctlr.updateRouteAdmitStatus(rscKey, "Failure while updating config", "Please check logs for more information", v1.ConditionFalse)
						//	} else {
						//		go ctlr.updateRouteAdmitStatus(rscKey, "", "", v1.ConditionTrue)
						//	}
					}
				}
			}
		}
	}
}

func (ctlr *Controller) removeUnusedIPAMEntries(kind string) {
	// Remove Unused IPAM entries in IPAM CR after CIS restarts, applicable to only first PostCall
	if !ctlr.firstPostResponse && ctlr.ipamCli != nil && (kind == VirtualServer || kind == TransportServer) {
		ctlr.firstPostResponse = true
		toRemoveIPAMEntries := &ficV1.IPAM{
			ObjectMeta: metav1.ObjectMeta{
				Labels: make(map[string]string),
			},
		}
		ipamCR := ctlr.getIPAMCR()
		for _, hostSpec := range ipamCR.Spec.HostSpecs {
			found := false
			ctlr.cacheIPAMHostSpecs.Lock()
			for cacheIndex, cachehostSpec := range ctlr.cacheIPAMHostSpecs.IPAM.Spec.HostSpecs {
				if (hostSpec.IPAMLabel == cachehostSpec.IPAMLabel && hostSpec.Host == cachehostSpec.Host) ||
					(hostSpec.IPAMLabel == cachehostSpec.IPAMLabel && hostSpec.Key == cachehostSpec.Key) ||
					(hostSpec.IPAMLabel == cachehostSpec.IPAMLabel && hostSpec.Key == cachehostSpec.Key && hostSpec.Host == cachehostSpec.Host) {
					if len(ctlr.cacheIPAMHostSpecs.IPAM.Spec.HostSpecs) > cacheIndex {
						ctlr.cacheIPAMHostSpecs.IPAM.Spec.HostSpecs = append(ctlr.cacheIPAMHostSpecs.IPAM.Spec.HostSpecs[:cacheIndex], ctlr.cacheIPAMHostSpecs.IPAM.Spec.HostSpecs[cacheIndex+1:]...)
					}
					found = true
					break
				}
			}
			ctlr.cacheIPAMHostSpecs.Unlock()
			if !found {
				// To remove
				toRemoveIPAMEntries.Spec.HostSpecs = append(toRemoveIPAMEntries.Spec.HostSpecs, hostSpec)
			}
		}
		for _, removeIPAMentry := range toRemoveIPAMEntries.Spec.HostSpecs {
			ipamCR = ctlr.getIPAMCR()
			for index, hostSpec := range ipamCR.Spec.HostSpecs {
				if (hostSpec.IPAMLabel == removeIPAMentry.IPAMLabel && hostSpec.Host == removeIPAMentry.Host) ||
					(hostSpec.IPAMLabel == removeIPAMentry.IPAMLabel && hostSpec.Key == removeIPAMentry.Key) ||
					(hostSpec.IPAMLabel == removeIPAMentry.IPAMLabel && hostSpec.Key == removeIPAMentry.Key && hostSpec.Host == removeIPAMentry.Host) {
					_, err := ctlr.RemoveIPAMCRHostSpec(ipamCR, removeIPAMentry.Key, index)
					if err != nil {
						log.Errorf("[IPAM] ipam hostspec update error: %v", err)
					}
					break
				}
			}
		}
		// Delete cacheIPAMHostSpecs
		ctlr.cacheIPAMHostSpecs = CacheIPAM{}
	}
}
