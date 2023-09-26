package main

import (
	"sync"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/spgyip/envoy-ads/apis/gengo/envoy/service/discovery/v3"
)

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
