package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/VictorLowther/jsonpatch"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/digitalrebar/digitalrebar/go/common/cert"
	"github.com/digitalrebar/digitalrebar/go/common/multi-tenancy"
	"github.com/digitalrebar/digitalrebar/go/common/store"
)

type NextServer struct {
	Server string `json:"next_server"`
}

type Frontend struct {
	DhcpInfo *DataTracker
	dataDir  string
}

func NewFrontend(store store.SimpleStore) *Frontend {
	fe := &Frontend{
		dataDir:  dataDir,
		DhcpInfo: NewDataTracker(store),
	}
	fe.DhcpInfo.Lock()
	fe.DhcpInfo.load_data()
	fe.DhcpInfo.Unlock()

	return fe
}

// List function
func (fe *Frontend) GetAllSubnets(w rest.ResponseWriter, r *rest.Request) {
	fe.DhcpInfo.Lock()
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	nets := make([]*Subnet, 0, len(fe.DhcpInfo.Subnets))
	for _, net := range fe.DhcpInfo.Subnets {
		if capMap.HasCapability(net.TenantId, "SUBNET_READ") {
			nets = append(nets, net)
		}
	}
	fe.DhcpInfo.Unlock()
	w.WriteJson(nets)
}

// Get function
func (fe *Frontend) GetSubnet(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	fe.DhcpInfo.Lock()
	subnet, found := fe.DhcpInfo.Subnets[subnetName]
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if found && capMap.HasCapability(subnet.TenantId, "SUBNET_READ") {
		fe.DhcpInfo.Unlock()
		w.WriteJson(subnet)
	} else {
		fe.DhcpInfo.Unlock()
		rest.Error(w, "Not Found", http.StatusNotFound)
	}
}

// Create function
func (fe *Frontend) CreateSubnet(w rest.ResponseWriter, r *rest.Request) {
	s := &Subnet{}
	if r.Body == nil {
		rest.Error(w, "Must have body", http.StatusBadRequest)
		return
	}
	if err := r.DecodeJsonPayload(s); err != nil {
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(s.TenantId, "SUBNET_CREATE") {
		rest.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	fe.DhcpInfo.Lock()
	if err, code := fe.DhcpInfo.AddSubnet(s); err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteJson(s)
}

// Update function
func (fe *Frontend) UpdateSubnet(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	s := &Subnet{}
	if r.Body == nil {
		rest.Error(w, "Must have body", http.StatusBadRequest)
		return
	}
	net, found := fe.DhcpInfo.Subnets[subnetName]
	if !found {
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(net.TenantId, "SUBNET_UPDATE") {
		if !capMap.HasCapability(net.TenantId, "SUBNET_READ") {
			rest.Error(w, "Not Found", http.StatusNotFound)
		} else {
			rest.Error(w, "Forbidden", http.StatusForbidden)
		}
		return
	}
	if err := r.DecodeJsonPayload(s); err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fe.DhcpInfo.Lock()
	if err, code := fe.DhcpInfo.ReplaceSubnet(subnetName, s); err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteJson(s)
}

func (fe *Frontend) PatchSubnet(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	s := &Subnet{}
	if r.Body == nil {
		rest.Error(w, "Must have body", http.StatusBadRequest)
		return
	}
	net, found := fe.DhcpInfo.Subnets[subnetName]
	if !found {
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(net.TenantId, "SUBNET_UPDATE") {
		if !capMap.HasCapability(net.TenantId, "SUBNET_READ") {
			rest.Error(w, "Not Found", http.StatusNotFound)
		} else {
			rest.Error(w, "Forbidden", http.StatusForbidden)
		}
		return
	}
	patch, err := ioutil.ReadAll(r.Body)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	oldBuf, _ := json.Marshal(net)
	newBuf, err, loc := jsonpatch.ApplyJSON(oldBuf, patch)
	if err != nil {
		rest.Error(w, fmt.Sprintf("Failed to apply patch at %d: %v", loc, err), http.StatusBadRequest)
		return
	}
	if err := json.Unmarshal(newBuf, s); err != nil {
		rest.Error(w, fmt.Sprintf("Patch created invalid object: %v", err), http.StatusBadRequest)
		return
	}
	fe.DhcpInfo.Lock()
	if err, code := fe.DhcpInfo.ReplaceSubnet(subnetName, s); err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteJson(s)
}

// Delete function
func (fe *Frontend) DeleteSubnet(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	fe.DhcpInfo.Lock()
	subnet, found := fe.DhcpInfo.Subnets[subnetName]
	if !found {
		fe.DhcpInfo.Unlock()
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(subnet.TenantId, "SUBNET_DESTROY") {
		if !capMap.HasCapability(subnet.TenantId, "SUBNET_READ") {
			rest.Error(w, "Not Found", http.StatusNotFound)
		} else {
			rest.Error(w, "Forbidden", http.StatusForbidden)
		}
		fe.DhcpInfo.Unlock()
		return
	}
	err, code := fe.DhcpInfo.RemoveSubnet(subnetName)
	if err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteHeader(code)
}

func (fe *Frontend) BindSubnet(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	binding := Binding{}
	if r.Body == nil {
		rest.Error(w, "Must have body", http.StatusBadRequest)
		return
	}
	err := r.DecodeJsonPayload(&binding)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	binding.Mac = strings.ToLower(binding.Mac)
	fe.DhcpInfo.Lock()

	subnet, found := fe.DhcpInfo.Subnets[subnetName]
	if !found {
		fe.DhcpInfo.Unlock()
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(subnet.TenantId, "SUBNET_UPDATE") {
		if !capMap.HasCapability(subnet.TenantId, "SUBNET_READ") {
			rest.Error(w, "Not Found", http.StatusNotFound)
		} else {
			rest.Error(w, "Forbidden", http.StatusForbidden)
		}
		fe.DhcpInfo.Unlock()
		return
	}

	err, code := fe.DhcpInfo.AddBinding(subnetName, binding)
	if err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteJson(binding)
}

func (fe *Frontend) UnbindSubnet(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	mac := r.PathParam("mac")
	mac = strings.ToLower(mac)
	fe.DhcpInfo.Lock()

	subnet, found := fe.DhcpInfo.Subnets[subnetName]
	if !found {
		fe.DhcpInfo.Unlock()
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(subnet.TenantId, "SUBNET_UPDATE") {
		if !capMap.HasCapability(subnet.TenantId, "SUBNET_READ") {
			rest.Error(w, "Not Found", http.StatusNotFound)
		} else {
			rest.Error(w, "Forbidden", http.StatusForbidden)
		}
		fe.DhcpInfo.Unlock()
		return
	}

	err, code := fe.DhcpInfo.DeleteBinding(subnetName, mac)
	if err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (fe *Frontend) NextServer(w rest.ResponseWriter, r *rest.Request) {
	subnetName := r.PathParam("id")
	nextServer := NextServer{}
	if r.Body == nil {
		rest.Error(w, "Must have body", http.StatusBadRequest)
		return
	}
	if err := r.DecodeJsonPayload(&nextServer); err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ip := net.ParseIP(r.PathParam("ip"))
	fe.DhcpInfo.Lock()

	subnet, found := fe.DhcpInfo.Subnets[subnetName]
	if !found {
		fe.DhcpInfo.Unlock()
		rest.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	capMap, err := multitenancy.NewCapabilityMap(r.Request)
	if err != nil {
		log.Printf("Failed to get capmap from request: %v\n", err)
		rest.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !capMap.HasCapability(subnet.TenantId, "SUBNET_UPDATE") {
		if !capMap.HasCapability(subnet.TenantId, "SUBNET_READ") {
			rest.Error(w, "Not Found", http.StatusNotFound)
		} else {
			rest.Error(w, "Forbidden", http.StatusForbidden)
		}
		fe.DhcpInfo.Unlock()
		return
	}

	if err, code := fe.DhcpInfo.SetNextServer(subnetName, ip, nextServer); err != nil {
		fe.DhcpInfo.Unlock()
		rest.Error(w, err.Error(), code)
		return
	}
	fe.DhcpInfo.Unlock()
	w.WriteJson(nextServer)
}

func (fe *Frontend) RunServer(blocking bool) http.Handler {
	api := rest.NewApi()
	api.Use(&rest.AccessLogApacheMiddleware{},
		&rest.TimerMiddleware{},
		&rest.RecorderMiddleware{},
		&rest.PoweredByMiddleware{},
		&rest.RecoverMiddleware{
			EnableResponseStackTrace: true,
		},
		&rest.JsonIndentMiddleware{},
	)
	router, err := rest.MakeRouter(
		rest.Get("/subnets", fe.GetAllSubnets),
		rest.Get("/subnets/#id", fe.GetSubnet),
		rest.Post("/subnets", fe.CreateSubnet),
		rest.Put("/subnets/#id", fe.UpdateSubnet),
		rest.Patch("/subnets/#id", fe.PatchSubnet),
		rest.Delete("/subnets/#id", fe.DeleteSubnet),
		rest.Post("/subnets/#id/bind", fe.BindSubnet),
		rest.Delete("/subnets/#id/bind/#mac", fe.UnbindSubnet),
		rest.Put("/subnets/#id/next_server/#ip", fe.NextServer),
	)
	if err != nil {
		log.Fatal(err)
	}
	api.SetApp(router)

	if !blocking {
		return api.MakeHandler()
	}

	connStr := fmt.Sprintf(":%d", serverPort)
	log.Println("Web Interface Using", connStr)
	acceptingRoot := "internal"
	hosts := strings.Split(hostString, ",")
	log.Fatal(cert.StartTLSServer(connStr, "dhcp-mgmt", hosts, acceptingRoot, "internal", api.MakeHandler()))

	// Should never hit here.
	return api.MakeHandler()
}
