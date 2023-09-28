package main

import (
	"fmt"
	"net"
	"reflect"

	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"github.com/spgyip/olayc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

const (
	// Default listen address
	defaultAddr = ":10025"
)

var (
	// Program name
	program = "Ads"
	// Ads version
	// Set with ``go build -ldflags="-X main.version=..."``
	version = "unknown"

	// Release built
	// Set with ``go build -ldflags="-X main.build=..."``
	build = "debug"
)

var g_lls *localListenerState

func main() {
	olayc.Load(
		olayc.WithUsage("l", reflect.String, defaultAddr, "Listen address."),
		olayc.WithUsage("v", reflect.Bool, false, "Show version."),
	)
	addr := olayc.String("l", defaultAddr)
	showV := olayc.Bool("v", false)

	if showV {
		fmt.Printf("%v: %v\n", program, version)
		fmt.Printf("Build: %v\n", build)
		return
	}

	var logger *zap.Logger
	if build != "debug" {
		logger, _ = zap.NewProduction()
	} else {
		logger, _ = zap.NewDevelopment()
	}
	defer logger.Sync()
	loggerS := logger.Sugar()

	var err error
	g_lls, err = newLocalListenerState(logger)
	if err != nil {
		loggerS.Fatalw("NewLocalListenerState error", "error", err)
	}

	loggerS.Infow("Listening address", "addr", addr)
	s := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(s, &aggDiscoveryServerImpl{logger: logger})
	ls, err := net.Listen("tcp", addr)
	if err != nil {
		loggerS.Fatalw("Fail to listen", "error", err)
	}
	if err := s.Serve(ls); err != nil {
		loggerS.Fatalw("Fail to serve", "error", err)
	}
}
