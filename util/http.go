package util

import (
    "bufio"
    "crypto/tls"
    "fmt"
    "io"
    "math/rand"
    "os"
    "net"
    "net/http"
    "strings"
    "sync"

    "github.com/lucas-clemente/quic-go"
    "github.com/lucas-clemente/quic-go/http3"

    "inet.af/netaddr"
)

type Network int
const (
    NetworkAny  Network = iota
    NetworkIPv4
    NetworkIPv6
)

var closeRequested = fmt.Errorf("Client close requested")

type IPPool struct {
    Addresses []netaddr.IP
}

func ParseIPPool(path string) (*IPPool, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    pool := &IPPool {}
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }
        ip, err := netaddr.ParseIP(line)
        if err != nil {
            return nil, err
        }
        pool.Addresses = append(pool.Addresses, ip)
    }
    if err = scanner.Err(); err != nil {
        return nil, err
    }
    return pool, nil
}

func (p *IPPool) random() netaddr.IP {
    if len(p.Addresses) == 0 {
        panic("No IP addresses in pool")
    }
    return p.Addresses[rand.Intn(len(p.Addresses))]
}

type HttpClientConfig struct {
    IPPool  *IPPool
    Network Network
    UseQuic bool
}

type HttpClient struct {
    cfg        *HttpClientConfig
    socketLock sync.Mutex
    sockets    map[netaddr.IP]quic.OOBCapablePacketConn
    clientLock sync.Mutex
    clients    map[netaddr.IP]*internalClient
    // used for NetworkAny
    anyClient  *internalClient
}

func NewClient(cfg *HttpClientConfig) *HttpClient {
    return &HttpClient {
        cfg: cfg,
    }
}

func (c *HttpClient) GetRequester() *HttpRequester {
    var bindAddr *netaddr.IP
    if c.cfg.IPPool != nil {
        a := c.cfg.IPPool.random()
        bindAddr = &a
    } else {
        switch c.cfg.Network {
        case NetworkAny:
            bindAddr = nil
        case NetworkIPv4:
            a := netaddr.IPv4(0, 0, 0, 0)
            bindAddr = &a
        case NetworkIPv6:
            a := netaddr.IPv6Unspecified()
            bindAddr = &a
        default:
            panic(fmt.Sprintf("Unhandled network type %v", c.cfg.Network))
        }
    }
    return &HttpRequester {
        owner:  c,
        client: c.getClient(bindAddr),
        ip:     bindAddr,
    }
}

func (c *HttpClient) getSocket(ip netaddr.IP) (quic.OOBCapablePacketConn, error) {
    c.socketLock.Lock()
    defer c.socketLock.Unlock()
    if c.sockets == nil {
        c.sockets = make(map[netaddr.IP]quic.OOBCapablePacketConn)
    }
    if conn, ok := c.sockets[ip]; ok {
        return conn, nil
    }

    var network string
    var addr net.IP
    if ip.Is6() {
        network = "udp6"
        ip6 := ip.As16()
        addr = append([]byte(nil), ip6[:]...)
    } else {
        network = "udp4"
        ip4 := ip.As4()
        addr = append([]byte(nil), ip4[:]...)
    }

    udpConn, err := net.ListenUDP(network, &net.UDPAddr{IP: addr, Port: 0})
    if err != nil {
        return nil, err
    }
    c.sockets[ip] = udpConn
    return udpConn, nil
}

func (c *HttpClient) dropClient(ip *netaddr.IP, expectedClient *internalClient) {
    c.clientLock.Lock()
    defer c.clientLock.Unlock()

    if ip == nil {
        if c.anyClient == expectedClient {
            c.anyClient.startClose()
            c.anyClient = nil
        }
        return
    }

    if c.clients == nil {
        panic("Attempt to drop client but map is nil")
    }

    client := c.clients[*ip]
    if client == expectedClient {
        client.startClose()
        c.clients[*ip] = nil
    }
}

func (c *HttpClient) getClient(ip *netaddr.IP) *internalClient {
    c.clientLock.Lock()
    defer c.clientLock.Unlock()

    if ip == nil {
        if c.anyClient == nil {
            c.anyClient = c.createClient(nil)
        }
        return c.anyClient
    }

    if c.clients == nil {
        c.clients = make(map[netaddr.IP]*internalClient)
    }

    client := c.clients[*ip]
    if client != nil {
        return client
    }

    client = c.createClient(func(network, addr string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlySession, error) {
        if ip.Is6() {
            network = "udp6"
        } else {
            network = "udp4"
        }

        remoteAddr, err := net.ResolveUDPAddr(network, addr)
        if err != nil {
            return nil, err
        }
        udpConn, err := c.getSocket(*ip)
        if err != nil {
            return nil, err
        }
        return quic.DialEarly(udpConn, remoteAddr, addr, tlsCfg, cfg)
    })
    c.clients[*ip] = client
    return client
}

func (c *HttpClient) createClient(dial func(network, addr string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlySession, error)) *internalClient {
    var rt http.RoundTripper
    if c.cfg.UseQuic {
        rt = &http3.RoundTripper {
            Dial: dial,
        }
    }
    return &internalClient {
        client: &http.Client {
            Transport: rt,
        },
    }
}

type HttpRequester struct {
    owner  *HttpClient
    client *internalClient
    ip     *netaddr.IP
    mu     sync.Mutex
}

func (r *HttpRequester) Dispose() {
    r.mu.Lock()
    defer r.mu.Unlock()

    r.owner.dropClient(r.ip, r.client)
    r.client = nil
}

func (r *HttpRequester) Do(req *http.Request) (*http.Response, error) {
    r.mu.Lock()
    c := r.client
    r.mu.Unlock()

    if c == nil {
        panic("Use of disposed requester")
    }
    return c.do(req)
}

func (r *HttpRequester) Get(url string) (*http.Response, error) {
    req, err := http.NewRequest("GET", url, nil)
    if err != nil {
        return nil, err
    }
    return r.Do(req)
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
    if cl, ok := c.client.Transport.(io.Closer); ok {
        cl.Close()
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
    if c.shouldClose && c.pendingRequests == 0 {
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

