package main

import (
	"sync"
	"unsafe"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/pkg/errors"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// Local listener state
type localListenerState struct {
	version string
	host    string
	port    uint32

	// DiscoveryResponse could be built once because the listener state is static
	resp *discoveryv3.DiscoveryResponse

	mu   sync.Mutex
	subs map[uintptr]*subscriber

	// Notify state changed or subscriber changed
	notifyCh chan bool
}

// TODO: New with config
func newLocalListenerState() (*localListenerState, error) {
	s := &localListenerState{
		version:  "1",
		host:     "127.0.0.1",
		port:     9001,
		subs:     make(map[uintptr]*subscriber),
		notifyCh: make(chan bool, 1),
	}
	if err := s.buildResponse(); err != nil {
		return nil, errors.Wrap(err, "Build response error")
	}
	s.run()
	return s, nil
}

func (s *localListenerState) run() {
	go func() {
		for {
			<-s.notifyCh
			zapS.Info("LocalListernerState notify fired, check subscribers and publish resources")
			s.broadcast()
		}
	}()
}

func (s *localListenerState) broadcast() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		if sub.getListenerResourceVersion() < s.version {
			sub.pushResource(s.resp)
		}
	}
}

func (s *localListenerState) notify() {
	select {
	case s.notifyCh <- true:
	default:
	}
}

func (s *localListenerState) addSubscriber(sub *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ptr := uintptr(unsafe.Pointer(sub))
	if _, ok := s.subs[ptr]; !ok {
		s.subs[ptr] = sub
	}
}

func (s *localListenerState) removeSubscriber(sub *subscriber) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ptr := uintptr(unsafe.Pointer(sub))
	if _, ok := s.subs[ptr]; ok {
		delete(s.subs, ptr)
		return true
	}
	return false
}

func (s *localListenerState) buildResponse() error {
	var err error
	var httpConnMgrProtoByte []byte
	var listenerProtoByte []byte

	// HttpConectionManager
	httpConnMgr := &hcmv3.HttpConnectionManager{
		CodecType:  hcmv3.HttpConnectionManager_HTTP2,
		StatPrefix: "egress_http",
		RouteSpecifier: &hcmv3.HttpConnectionManager_Rds{
			Rds: &hcmv3.Rds{
				RouteConfigName: "egress_route",
				ConfigSource: &corev3.ConfigSource{
					ConfigSourceSpecifier: &corev3.ConfigSource_Ads{},
				},
			},
		},
	}
	httpConnMgrProtoByte, err = proto.Marshal(httpConnMgr)
	if err != nil {
		return errors.Wrap(err, "Marshal HttpConnectionManager proto error")
	}

	// Filter
	filter := &listenerv3.Filter{
		Name: "egress_filter",
	}
	filter.ConfigType = &listenerv3.Filter_TypedConfig{
		TypedConfig: &anypb.Any{
			TypeUrl: "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
			Value:   httpConnMgrProtoByte,
		},
	}

	// Listener
	lst := &listenerv3.Listener{
		Name: "egress_listener",
		Address: &corev3.Address{
			Address: &corev3.Address_SocketAddress{
				SocketAddress: &corev3.SocketAddress{
					Protocol: corev3.SocketAddress_TCP,
					Address:  s.host,
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: s.port,
					},
				},
			},
		},
		StatPrefix: "egress_listerner",
	}
	lst.FilterChains = make([]*listenerv3.FilterChain, 1)
	lst.FilterChains[0] = &listenerv3.FilterChain{
		Name: "egress_filter_chain",
		Filters: []*listenerv3.Filter{
			filter,
		},
	}

	listenerProtoByte, err = proto.Marshal(lst)
	if err != nil {
		return errors.Wrap(err, "Marshal protobuf error")
	}

	// DiscoveryResponse
	s.resp = &discoveryv3.DiscoveryResponse{
		VersionInfo: s.version,
		TypeUrl:     "type.googleapis.com/envoy.config.listener.v3.Listener",
		Resources: []*anypb.Any{
			&anypb.Any{
				TypeUrl: "type.googleapis.com/envoy.config.listener.v3.Listener",
				Value:   listenerProtoByte,
			},
		},
	}

	return nil
}
