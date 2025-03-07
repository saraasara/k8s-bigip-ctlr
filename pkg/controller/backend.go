/*-
 * Copyright (c) 2016-2021, F5 Networks, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controller

import (
	"fmt"
	log "github.com/F5Networks/k8s-bigip-ctlr/v3/pkg/vlogger"
	"sort"
	"strconv"
	"strings"
)

var baseAS3Config = `{
	"$schema": "https://raw.githubusercontent.com/F5Networks/f5-appsvcs-extension/master/schema/%s/as3-schema-%s.json",
	"class": "AS3",
	"declaration": {
	  "class": "ADC",
	  "schemaVersion": "3.0.0",
	  "id": "urn:uuid:85626792-9ee7-46bb-8fc8-4ba708cfdc1d",
	  "label": "CIS Declaration",
	  "remark": "Auto-generated by CIS",
	  "controls": {
		 "class": "Controls",
		 "userAgent": "CIS Configured AS3"
	  }
	}
  }
`

var baseAS3Config2 = `{
	  "class": "ADC",
	  "schemaVersion": "3.0.0",
	  "id": "urn:uuid:85626792-9ee7-46bb-8fc8-4ba708cfdc1d",
	  "label": "CIS Declaration",
	  "remark": "Auto-generated by CIS",
	  "controls": {
		 "class": "Controls",
		 "userAgent": "CIS Configured AS3"
	  }
  }
`

var DEFAULT_PARTITION string
var DEFAULT_GTM_PARTITION string

// Extract virtual address and port from host URL
func extractVirtualAddressAndPort(str string) (string, int) {

	destination := strings.Split(str, "/")
	// split separator is in accordance with SetVirtualAddress function - ipv4/6 format
	ipPort := strings.Split(destination[len(destination)-1], ":")
	if len(ipPort) != 2 {
		ipPort = strings.Split(destination[len(destination)-1], ".")
	}
	// verify that ip address and port exists else log error.
	if len(ipPort) == 2 {
		port, _ := strconv.Atoi(ipPort[1])
		return ipPort[0], port
	} else {
		log.Error("Invalid Virtual Server Destination IP address/Port.")
		return "", 0
	}

}

func createTLSClient(
	prof CustomProfile,
	svcName, caBundleName string,
	sharedApp as3Application,
) *as3TLSClient {

	// For TLSClient only Cert (DestinationCACertificate) is given and key is empty string
	for _, certificate := range prof.Certificates {
		if certificate.Key != "" {
			return nil
		}
	}
	if _, ok := sharedApp[svcName]; len(prof.Certificates) > 0 && ok {
		svc := sharedApp[svcName].(*as3Service)
		tlsClientName := fmt.Sprintf("%s_tls_client", svcName)

		tlsClient := &as3TLSClient{
			Class: "TLS_Client",
			TrustCA: &as3ResourcePointer{
				Use: caBundleName,
			},
		}
		if prof.CipherGroup != "" {
			tlsClient.CipherGroup = &as3ResourcePointer{BigIP: prof.CipherGroup}
			tlsClient.TLS1_3Enabled = true
		} else {
			tlsClient.Ciphers = prof.Ciphers
		}
		sharedApp[tlsClientName] = tlsClient
		svc.ClientTLS = tlsClientName
		updateVirtualToHTTPS(svc)

		return tlsClient
	}
	return nil
}

// getSortedCustomProfileKeys sorts customProfiles by names and returns secretKeys in that order
func getSortedCustomProfileKeys(customProfiles map[SecretKey]CustomProfile) []SecretKey {
	keys := make([]SecretKey, len(customProfiles))
	i := 0
	for key := range customProfiles {
		keys[i] = key
		i++
	}
	sort.Slice(keys, func(i, j int) bool {
		return customProfiles[keys[i]].Name < customProfiles[keys[j]].Name
	})
	return keys
}
