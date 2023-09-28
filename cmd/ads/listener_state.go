package main

import (
	"sync"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/pkg/errors"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type localState interface {
	// Add subscriber if not exists
	// Return false if already exists
	AddSubscriber(subscriber) bool
	// Remove subscriber if exists
	// Return false if already exists
	RemoveSubscriber(subscriber) bool
}

// Local listener state
type localListenerState struct {
	version string
	host    string
	port    uint32

	// DiscoveryResponse could be built once because the listener state is static
	resp *discoveryv3.DiscoveryResponse

	mu   sync.Mutex
	subs map[uint64]subscriber

	// Notify state changed or subscriber changed
	notifyCh chan bool

	logger *zap.Logger
}

// TODO: New with config
func newLocalListenerState(logger *zap.Logger) (*localListenerState, error) {
	ls := &localListenerState{
		version:  "1",
		host:     "127.0.0.1",
		port:     9001,
		subs:     make(map[uint64]subscriber),
		notifyCh: make(chan bool, 1),
		logger:   logger,
	}
	if err := ls.buildResponse(); err != nil {
		return nil, errors.Wrap(err, "Build response error")
	}
	ls.run()
	return ls, nil
}

func (ls *localListenerState) AddSubscriber(sub subscriber) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	sid := sub.Id()
	if _, ok := ls.subs[sid]; !ok {
		ls.subs[sid] = sub
		return true
	}
	return false
}

func (ls *localListenerState) RemoveSubscriber(sub subscriber) bool {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	sid := sub.Id()
	if _, ok := ls.subs[sid]; ok {
		delete(ls.subs, sid)
		return true
	}
	return false
}

func (ls *localListenerState) run() {
	go func() {
		for {
			<-ls.notifyCh
			ls.logger.Debug("LocalListernerState notify fired, check subscribers and publish resources")
			ls.broadcast()
		}
	}()
}

func (ls *localListenerState) broadcast() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	for _, sub := range ls.subs {
		sub.PublishListener(ls.version, ls.resp)
	}
}

func (ls *localListenerState) notify() {
	select {
	case ls.notifyCh <- true:
	default:
	}
}

func (ls *localListenerState) buildResponse() error {
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
					Address:  ls.host,
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: ls.port,
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
	ls.resp = &discoveryv3.DiscoveryResponse{
		VersionInfo: ls.version,
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
