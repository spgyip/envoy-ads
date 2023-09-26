package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"unsafe"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	http_connection_managerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/pkg/errors"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"github.com/spgyip/olayc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func retriveUserAgentVersion(node *corev3.Node) string {
	userAgentVersion := "Unknown"
	if node != nil {
		switch uavt := node.UserAgentVersionType.(type) {
		case *corev3.Node_UserAgentVersion:
			userAgentVersion = fmt.Sprintf("UserAgentVersion(%v)", uavt.UserAgentVersion)
		case *corev3.Node_UserAgentBuildVersion:
			userAgentVersion = fmt.Sprintf("UserAgentBuildVersion(%v.%v.%v)",
				uavt.UserAgentBuildVersion.Version.MajorNumber,
				uavt.UserAgentBuildVersion.Version.MinorNumber,
				uavt.UserAgentBuildVersion.Version.Patch,
			)
		}
	}
	return userAgentVersion
}

type aggDiscoveryServerImpl struct {
	discoveryv3.UnimplementedAggregatedDiscoveryServiceServer
}

type subscriber struct {
	// ``mu`` protect data(s) in ``subscriber``
	mu                      sync.RWMutex
	node                    *corev3.Node
	listenerResourceVersion string
	clusterResourceVersion  string

	respCh  chan *discoveryv3.DiscoveryResponse
	closeCh chan bool
	stream  discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer
}

func newSubscriber(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) *subscriber {
	s := &subscriber{
		stream:  stream,
		closeCh: make(chan bool),
		respCh:  make(chan *discoveryv3.DiscoveryResponse, 1),
	}
	s.run()
	return s
}

func (s *subscriber) run() {
	go func() {
		running := true
		for running {
			select {
			case resp := <-s.respCh:
				zapS.Info("Sending DiscoveryResponse on stream")
				if err := s.stream.Send(resp); err != nil {
					running = false
				}
			case <-s.closeCh:
				running = false
			}
		}
		zapS.Info("Subscriber is closing")
	}()
}

func (s *subscriber) close() {
	close(s.closeCh)
}

func (s *subscriber) getNode() *corev3.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.node
}

func (s *subscriber) setNode(node *corev3.Node) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.node = node
}

func (s *subscriber) getListenerResourceVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listenerResourceVersion
}

func (s *subscriber) setListenerResourceVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listenerResourceVersion = version
}

func (s *subscriber) getClusterResourceVersion() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clusterResourceVersion
}

func (s *subscriber) setClusterResourceVersion(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterResourceVersion = version
}

func (s *subscriber) pushResource(resp *discoveryv3.DiscoveryResponse) {
	s.respCh <- resp
}

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
	httpConnMgr := &http_connection_managerv3.HttpConnectionManager{
		CodecType:  http_connection_managerv3.HttpConnectionManager_HTTP2,
		StatPrefix: "egress_http",
		RouteSpecifier: &http_connection_managerv3.HttpConnectionManager_Rds{
			Rds: &http_connection_managerv3.Rds{
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

func (s *aggDiscoveryServerImpl) StreamAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	sub := newSubscriber(stream)
	defer func() {
		zapS.Infow("End of rpc, close read/write and remove subscriber from localState(s)")
		sub.close()
		lls.removeSubscriber(sub)
	}()

	for {
		req, err := stream.Recv()
		if err != nil {
			zapS.Errorw(
				"Stream recv error, end the RPC",
				"error", err,
			)
			if err == io.EOF {
				return nil
			}
			return err
		}

		zapS.Infow(
			"DiscoveryRequest",
			"VersionInfo", req.VersionInfo,
			"ResourceNames", req.ResourceNames,
			"TypeUrl", req.TypeUrl,
			"ResponseNonce", req.ResponseNonce,
		)

		if sub.getNode() == nil {
			zapS.Infow("Node",
				"ID", req.Node.Id,
				"Cluster", req.Node.Cluster,
				"Locality", req.Node.Locality,
				"UserAgent", req.Node.UserAgentName,
				"UserAgentVersion", retriveUserAgentVersion(req.Node),
			)
			sub.setNode(req.Node)
		}

		if req.TypeUrl == "type.googleapis.com/envoy.config.listener.v3.Listener" {
			zapS.Infow("Subscribe listener resource with version",
				"version", req.VersionInfo)
			sub.setListenerResourceVersion(req.VersionInfo)
			lls.addSubscriber(sub)
			lls.notify()
		}
		/*
			switch req.TypeUrl {
			case "type.googleapis.com/envoy.config.listener.v3.Listener":
				err = handleDiscoveryListener(stream)
			case "type.googleapis.com/envoy.config.cluster.v3.Cluster":
				err = handleDiscoveryCluster(stream)
			}

			if err != nil {
				zapS.Errorw("Handle error, end the RPC", "error", err)
				return err
			}
		*/
	}
}

func handleDiscoveryCluster(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	zapS.Info("Handling discovery cluster...")
	// TODO
	return nil
}

func (s *aggDiscoveryServerImpl) DeltaAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		log.Printf("DeltaDiscoveryRequest: %v\n", req)
	}
}

const (
	// Default listen address
	defaultAddr = ":10025"
)

var (
	// Program name
	program = "ads"
	// Ads version
	// Set with ``go build -ldflags="-X main.version=..."``
	version = "unknown"
)

var zapL *zap.Logger
var zapS *zap.SugaredLogger

var lls *localListenerState

func main() {
	olayc.Load(
		olayc.WithUsage("l", reflect.String, defaultAddr, "Listen address."),
		olayc.WithUsage("v", reflect.Bool, false, "Show version."),
	)
	addr := olayc.String("l", defaultAddr)
	showV := olayc.Bool("v", false)

	if showV {
		fmt.Printf("%v: %v\n", program, version)
		return
	}

	var err error
	lls, err = newLocalListenerState()
	if err != nil {
		zapS.Fatalw("NewLocalListenerState error", "error", err)
	}

	zapL, _ = zap.NewProduction()
	zapS = zapL.Sugar()
	defer zapL.Sync()

	s := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(s, &aggDiscoveryServerImpl{})

	zapS.Infow("Listening address", "addr", addr)
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		zapS.Fatalw("Fail to listen", "error", err)
	}
	if err := s.Serve(ls); err != nil {
		zapS.Fatalw("Fail to serve", "error", err)
	}
}
