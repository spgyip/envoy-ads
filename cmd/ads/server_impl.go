package main

import (
	"fmt"
	"io"
	"log"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"go.uber.org/zap"
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
	logger *zap.Logger
}

func (s *aggDiscoveryServerImpl) StreamAggregatedResources(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	sub := newAggStreamSubscriber(stream, s.logger)
	defer func() {
		s.logger.Info("End of StreamAggregatedResources, close read/write and remove subscriber from localState(s)")
		sub.Close()
	}()

	for {
		req, err := stream.Recv()
		if err != nil {
			s.logger.Error(
				"ResourceStream recv error, end the RPC",
				zap.Error(err),
			)
			if err == io.EOF {
				return nil
			}
			return err
		}

		if sub.SetNode(req.Node) {
			s.logger.Debug("Node establishs a new stream",
				zap.String("ID", req.Node.Id),
				zap.String("Cluster", req.Node.Cluster),
				zap.Any("Locality", req.Node.Locality),
				zap.String("UserAgent", req.Node.UserAgentName),
				zap.String("UserAgentVersion", retriveUserAgentVersion(req.Node)),
			)
		}

		s.logger.Debug(
			"DiscoveryRequest",
			zap.String("VersionInfo", req.VersionInfo),
			zap.Any("ResourceNames", req.ResourceNames),
			zap.String("TypeUrl", req.TypeUrl),
			zap.String("ResponseNonce", req.ResponseNonce),
		)

		if req.TypeUrl == "type.googleapis.com/envoy.config.listener.v3.Listener" {
			s.logger.Debug("Subscribe listener resource with version",
				zap.String("version", req.VersionInfo),
			)
			sub.WatchListener(g_lls, req.VersionInfo)
			g_lls.notify()
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
