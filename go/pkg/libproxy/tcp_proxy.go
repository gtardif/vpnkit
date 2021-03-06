package libproxy

import (
	"fmt"
	"io"
	"log"
	"net"
)

// Conn defines a network connection
type Conn interface {
	io.Reader
	io.Writer
	io.Closer
	CloseRead() error
	CloseWrite() error
}

// TCPProxy is a proxy for TCP connections. It implements the Proxy interface to
// handle TCP traffic forwarding between the frontend and backend addresses.
type TCPProxy struct {
	listener     net.Listener
	frontendAddr net.Addr
	backendAddr  *net.TCPAddr
}

// NewTCPProxy creates a new TCPProxy.
func NewTCPProxy(listener net.Listener, backendAddr *net.TCPAddr) (*TCPProxy, error) {
	// If the port in frontendAddr was 0 then ListenTCP will have a picked
	// a port to listen on, hence the call to Addr to get that actual port:
	return &TCPProxy{
		listener:     listener,
		frontendAddr: listener.Addr(),
		backendAddr:  backendAddr,
	}, nil
}

// HandleTCPConnection forwards the TCP traffic to a specified backend address
func HandleTCPConnection(client Conn, backendAddr *net.TCPAddr, quit chan struct{}) error {
	backend, err := net.DialTCP("tcp", nil, backendAddr)
	if err != nil {
		return fmt.Errorf("Can't forward traffic to backend tcp/%v: %s\n", backendAddr, err)
	}

	event := make(chan int64)
	var broker = func(to, from Conn) {
		written, err := io.Copy(to, from)
		if err != nil {
			log.Println("error copying:", err)
		}
		err = from.CloseRead()
		if err != nil {
			log.Println("error CloseRead from:", err)
		}
		err = to.CloseWrite()
		if err != nil {
			log.Println("error CloseWrite to:", err)
		}
		event <- written
	}

	go broker(client, backend)
	go broker(backend, client)

	var transferred int64
	for i := 0; i < 2; i++ {
		select {
		case written := <-event:
			transferred += written
		case <-quit:
			// Interrupt the two brokers and "join" them.
			backend.Close()
			for ; i < 2; i++ {
				transferred += <-event
			}
			return nil
		}
	}
	backend.Close()
	return nil
}

// Run starts forwarding the traffic using TCP.
func (proxy *TCPProxy) Run() {
	quit := make(chan struct{})
	defer close(quit)
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			log.Printf("Stopping proxy on tcp/%v for tcp/%v (%s)", proxy.frontendAddr, proxy.backendAddr, err)
			return
		}
		go HandleTCPConnection(client.(Conn), proxy.backendAddr, quit)
	}
}

// Close stops forwarding the traffic.
func (proxy *TCPProxy) Close() { proxy.listener.Close() }

// FrontendAddr returns the TCP address on which the proxy is listening.
func (proxy *TCPProxy) FrontendAddr() net.Addr { return proxy.frontendAddr }

// BackendAddr returns the TCP proxied address.
func (proxy *TCPProxy) BackendAddr() net.Addr { return proxy.backendAddr }
