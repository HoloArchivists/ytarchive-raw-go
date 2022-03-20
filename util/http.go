package util

import (
    "crypto/tls"
    "fmt"
    "io"
    "net"
    "net/http"
    "sync"

    "github.com/lucas-clemente/quic-go"
    "github.com/lucas-clemente/quic-go/http3"
)

type Network int
const (
    NetworkAny  Network = iota
    NetworkIPv4
    NetworkIPv6
)

var closeRequested = fmt.Errorf("Client close requested")

type HttpClientConfig struct {
    Network Network
    UseQuic bool
}

type HttpClient struct {
    cfg    *HttpClientConfig
    client *internalClient
    mu     sync.Mutex
}

func NewClient(cfg *HttpClientConfig) *HttpClient {
    return &HttpClient {
        cfg: cfg,
    }
}

func (c *HttpClient) ReplaceClient() {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.client.startClose()
    c.client = nil
}

func (c *HttpClient) dialQuic(network, addr string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlySession, error) {
    var udpNetwork string
    var udpAddr    *net.UDPAddr
    //TODO ip pool?
    switch c.cfg.Network {
    case NetworkAny:
        udpNetwork = "udp"
        udpAddr    = nil
    case NetworkIPv4:
        udpNetwork = "udp4"
        udpAddr    = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
    case NetworkIPv6:
        udpNetwork = "udp6"
        udpAddr    = &net.UDPAddr{IP: net.IPv6zero, Port: 0}
    default:
        panic(fmt.Sprintf("Unhandled network type %v", c.cfg.Network))
    }
    remoteAddr, err := net.ResolveUDPAddr(udpNetwork, addr)
    if err != nil {
        return nil, err
    }
    udpConn, err := net.ListenUDP(udpNetwork, udpAddr)
    if err != nil {
        return nil, err
    }
    return quic.DialEarly(udpConn, remoteAddr, addr, tlsCfg, cfg)
}

func (c *HttpClient) getClient() *internalClient {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.client == nil {
        var rt http.RoundTripper
        if c.cfg.UseQuic {
            rt = &http3.RoundTripper {
                Dial: c.dialQuic,
            }
        }
        c.client = &internalClient {
            client: &http.Client {
                Transport: rt,
            },
        }
    }
    return c.client
}

func (c *HttpClient) Do(req *http.Request) (*http.Response, error) {
    //retry up to 3 times if a close was requested
    i := 0
    for {
        resp, err := c.getClient().do(req)
        if i < 3 && err == closeRequested {
            i++
            continue
        }
        return resp, err
    }
}

func (c *HttpClient) Get(url string) (*http.Response, error) {
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return nil, err
    }
    return c.Do(req)
}

// quic-go has no way to close the client without killing existing connections
// so instead closing here only requests that it gets closed later
type internalClient struct {
    client          *http.Client
    mu              sync.Mutex
    shouldClose     bool
    pendingRequests int
}

func (c *internalClient) doClose() {
    if c, ok := c.client.Transport.(io.Closer); ok {
        c.Close()
    }
}

func (c *internalClient) startClose() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.shouldClose = true
    if c.pendingRequests == 0 {
        c.doClose()
    }
}

func (c *internalClient) startRequest() bool {
    c.mu.Lock()
    defer c.mu.Unlock()

    if c.shouldClose {
        return false
    }
    c.pendingRequests++
    return true
}

func (c *internalClient) endRequest() {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.pendingRequests--
    if c.shouldClose {
        c.doClose()
    }
}

func (c *internalClient) do(req *http.Request) (*http.Response, error) {
    if !c.startRequest() {
        return nil, closeRequested
    }
    resp, err := c.client.Do(req)
    if resp != nil {
        resp.Body = &endReader {
            body:   resp.Body,
            client: c,
        }
    } else {
        c.endRequest()
    }
    return resp, err
}

type endReader struct {
    body   io.ReadCloser
    client *internalClient
    mu     sync.Mutex
}

func (r *endReader) Read(p []byte) (int, error) {
    return r.body.Read(p)
}

func (r *endReader) Close() error {
    err := r.body.Close()
    //guard against multiple closes
    r.mu.Lock()
    defer r.mu.Unlock()

    if r.client != nil {
        r.client.endRequest()
        r.client = nil
    }

    return err
}


