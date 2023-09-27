package main

import (
	"fmt"
	"io"
	"log"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
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
