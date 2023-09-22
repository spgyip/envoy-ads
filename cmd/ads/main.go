package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"reflect"

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

func (s *aggDiscoveryServerImpl) StreamAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			zapS.Errorw(
				"Stream recv error, end the RPC",
				"error", err,
			)
			if err == io.EOF {
				return nil
			} else {
				return err
			}
		}

		zapS.Infow(
			"DiscoveryRequest",
			"VersionInfo", req.VersionInfo,
			"ResourceNames", req.ResourceNames,
			"TypeUrl", req.TypeUrl,
		)
		if req.Node != nil {
			zapS.Infow("Node",
				"ID", req.Node.Id,
				"Cluster", req.Node.Cluster,
				"Locality", req.Node.Locality,
				"UserAgent", req.Node.UserAgentName,
				"UserAgentVersion", retriveUserAgentVersion(req.Node),
			)
		} else {
			zapS.Info("Node nil")
		}

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
	}
}

func handleDiscoveryListener(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	zapS.Info("Handling discovery listener...")
	var err error
	var httpConnMgrProtoByte []byte
	var listenerProtoByte []byte

	// HttpConectionManager
	httpConnMgr := &http_connection_managerv3.HttpConnectionManager{
		CodecType:  http_connection_managerv3.HttpConnectionManager_HTTP2,
		StatPrefix: "egress_http",
		RouteSpecifier: &http_connection_managerv3.HttpConnectionManager_Rds{
			Rds: &http_connection_managerv3.Rds{
				RouteConfigName: "default_http_route",
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
		Name: "http_connection_manager",
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
					Address:  "127.0.0.1",
					PortSpecifier: &corev3.SocketAddress_PortValue{
						PortValue: 9001,
					},
				},
			},
		},
		StatPrefix: "grpc_egress",
	}
	lst.FilterChains = make([]*listenerv3.FilterChain, 1)
	lst.FilterChains[0].Filters = make([]*listenerv3.Filter, 1)
	lst.FilterChains[0].Filters[0] = filter

	listenerProtoByte, err = proto.Marshal(lst)
	if err != nil {
		return errors.Wrap(err, "Marshal protobuf error")
	}

	// DiscoveryResponse
	resp := &discoveryv3.DiscoveryResponse{
		TypeUrl: "type.googleapis.com/envoy.config.listener.v3.Listener",
		Resources: []*anypb.Any{
			&anypb.Any{
				TypeUrl: "type.googleapis.com/envoy.config.listener.v3.Listener",
				Value:   listenerProtoByte,
			},
		},
	}

	// Send
	err = stream.Send(resp)
	if err != nil {
		return errors.Wrap(err, "Stream send DiscoveryResponse error")
	}
	log.Println("Response listener response success")
	return nil
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
	// Eds version
	// Set with go build -ldflags="-X main.version=..."
	version = "unknown"
)

var zapL *zap.Logger
var zapS *zap.SugaredLogger

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

	zapL, _ = zap.NewProduction()
	zapS = zapL.Sugar()
	defer zapL.Sync()

	s := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(s, &aggDiscoveryServerImpl{})

	zapS.Infow("Listening address", "addr", addr)
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		zapS.Fatalw("Fail to listen", "err", err)
	}
	if err := s.Serve(ls); err != nil {
		zapS.Fatalw("Fail to serve", "err", err)
	}
}
