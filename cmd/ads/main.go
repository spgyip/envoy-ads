package main

import (
	"io"
	"log"
	"net"
	"reflect"

	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"github.com/spgyip/olayc"
	"google.golang.org/grpc"
)

const (
	defaultAddr = ":10025"
)

type aggDiscoveryServerImpl struct {
	discoveryv3.UnimplementedAggregatedDiscoveryServiceServer
}

func (s *aggDiscoveryServerImpl) StreamAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		log.Printf("DiscoveryRequest: %v\n", req)
	}
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

func main() {
	olayc.Load(
		olayc.WithUsage("l", reflect.String, defaultAddr, "Listen address."),
	)
	addr := olayc.String("l", defaultAddr)

	s := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(s, &aggDiscoveryServerImpl{})

	log.Printf("Listening address %v\n", addr)
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Fail to listen: %v\n", err)
	}
	if err := s.Serve(ls); err != nil {
		log.Fatalf("Fail to serve: %v\n", err)
	}
}
