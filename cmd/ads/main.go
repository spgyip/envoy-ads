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
