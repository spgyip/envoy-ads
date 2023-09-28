package main

import (
	"sync"
	"sync/atomic"
	"unsafe"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
	"go.uber.org/zap"
)

type subscriber interface {
	// Return an unique identifier
	Id() uint64

	// Watch localState with resoure version
	WatchListener(localState, string)
	WatchCluster(localState, string)

	// Publish resource with state version
	// If state version is beyond to resource version, publish it and return true, else return false.
	PublishListener(string, *discoveryv3.DiscoveryResponse) bool
	PublishCluster(string, *discoveryv3.DiscoveryResponse) bool

	// Close the subscriber
	Close()
}

type aggStreamSubscriber struct {
	node                    atomic.Value
	listenerResourceVersion atomic.Value
	clusterResourceVersion  atomic.Value

	mu     sync.Mutex
	states []localState

	stream  discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer
	closeCh chan bool
	respCh  chan *discoveryv3.DiscoveryResponse

	logger *zap.Logger
}

func newAggStreamSubscriber(stream discoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesServer, logger *zap.Logger) *aggStreamSubscriber {
	s := &aggStreamSubscriber{
		states:  make([]localState, 0),
		stream:  stream,
		closeCh: make(chan bool),
		respCh:  make(chan *discoveryv3.DiscoveryResponse, 1),
		logger:  logger,
	}
	s.run()
	return s
}

func (s *aggStreamSubscriber) Id() uint64 {
	return uint64(uintptr(unsafe.Pointer(s)))
}

func (s *aggStreamSubscriber) WatchListener(ls localState, version string) {
	s.listenerResourceVersion.Store(version)
	if ok := ls.AddSubscriber(s); !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = append(s.states, ls)
}

func (s *aggStreamSubscriber) WatchCluster(ls localState, version string) {
	s.clusterResourceVersion.Store(version)
	if ok := ls.AddSubscriber(s); !ok {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.states = append(s.states, ls)
}

func (s *aggStreamSubscriber) PublishListener(version string, resp *discoveryv3.DiscoveryResponse) bool {
	sv := s.listenerResourceVersion.Load().(string)
	if sv >= version {
		s.logger.Debug("aggStreamSubscriber::PublishListener unchanged",
			zap.String("subscriber version", sv),
			zap.String("state version", version),
		)
		return false
	}
	s.respCh <- resp
	return true
}

func (s *aggStreamSubscriber) PublishCluster(version string, resp *discoveryv3.DiscoveryResponse) bool {
	sv := s.clusterResourceVersion.Load().(string)
	if sv >= version {
		s.logger.Debug("aggStreamSubscriber::PublishCluster unchanged",
			zap.String("subscriber version", sv),
			zap.String("state version", version),
		)
		return false
	}
	s.respCh <- resp
	return true
}

func (s *aggStreamSubscriber) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ls := range s.states {
		ls.RemoveSubscriber(s)
	}
	close(s.closeCh)
}

func (s *aggStreamSubscriber) run() {
	go func() {
		running := true
		for running {
			select {
			case resp := <-s.respCh:
				s.logger.Debug("Sending DiscoveryResponse on stream",
					zap.String("type", resp.TypeUrl),
					zap.String("version", resp.VersionInfo),
				)
				if err := s.stream.Send(resp); err != nil {
					running = false
				}
			case <-s.closeCh:
				running = false
			}
		}
		s.logger.Debug("Subscriber is closing")
	}()
}

// Set node should be only applied once
// Return false if already exists
func (s *aggStreamSubscriber) SetNode(node *corev3.Node) bool {
	v := s.node.Load()
	if v != nil {
		return false
	}
	s.node.Store(node)
	return true
}

// Get node
// Return nil if not exists
func (s *aggStreamSubscriber) GetNode() *corev3.Node {
	v := s.node.Load()
	if v == nil {
		return nil
	}
	return v.(*corev3.Node)
}
