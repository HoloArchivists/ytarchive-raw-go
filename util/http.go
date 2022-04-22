package util

import (
    "bufio"
    "context"
    "crypto/tls"
    "fmt"
    "io"
    "math/rand"
    "os"
    "net"
    "net/http"
    "strings"
    "sync"
    "time"

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

func netaddr2net(ip netaddr.IP) net.IP {
    if ip.Is6() {
        ip6 := ip.As16()
        return append([]byte(nil), ip6[:]...)
    } else {
        ip4 := ip.As4()
        return append([]byte(nil), ip4[:]...)
    }
}

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
    cfg            *HttpClientConfig
    socketsLock    sync.Mutex
    sockets        map[netaddr.IP]quic.OOBCapablePacketConn
    requestersLock sync.Mutex
    requesters     map[netaddr.IP]*HttpRequester
    // used for NetworkAny
    anyRequester   *HttpRequester
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
    c.requestersLock.Lock()
    defer c.requestersLock.Unlock()

    if bindAddr == nil {
        if c.anyRequester == nil {
            c.anyRequester = &HttpRequester {
                owner: c,
                ip:    nil,
            }
        }
        return c.anyRequester
    }

    if c.requesters == nil {
        c.requesters = make(map[netaddr.IP]*HttpRequester)
    }

    req := c.requesters[*bindAddr]
    if req == nil {
        req = &HttpRequester {
            owner: c,
            ip:    bindAddr,
        }
        c.requesters[*bindAddr] = req
    }
    return req
}

func (c *HttpClient) getSocket(ip netaddr.IP) (quic.OOBCapablePacketConn, error) {
    c.socketsLock.Lock()
    defer c.socketsLock.Unlock()
    if c.sockets == nil {
        c.sockets = make(map[netaddr.IP]quic.OOBCapablePacketConn)
    }
    if conn, ok := c.sockets[ip]; ok {
        return conn, nil
    }

    var network string
    if ip.Is6() {
        network = "udp6"
    } else {
        network = "udp4"
    }
    addr := netaddr2net(ip)

    udpConn, err := net.ListenUDP(network, &net.UDPAddr{IP: addr, Port: 0})
    if err != nil {
        return nil, err
    }
    c.sockets[ip] = udpConn
    return udpConn, nil
}

func (c *HttpClient) createClient(ip *netaddr.IP) *internalClient {
    var rt http.RoundTripper
    if c.cfg.UseQuic {
        t := &http3.RoundTripper {}
        if ip != nil {
            t.Dial = func(ctx context.Context, network, addr string, tlsCfg *tls.Config, cfg *quic.Config) (quic.EarlyConnection, error) {
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
            }
        }
        rt = t
    } else {
        t := http.DefaultTransport.(*http.Transport).Clone()
        if ip != nil {
            dialer := &net.Dialer{
                Timeout:   30 * time.Second,
                KeepAlive: 30 * time.Second,
                LocalAddr: &net.TCPAddr{IP: netaddr2net(*ip), Port: 0},
            }
            t.DialContext = dialer.DialContext
        }
        rt = t
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

    if r.client != nil {
        r.client.startClose()
        r.client = nil
    }
}

func (r *HttpRequester) Do(req *http.Request) (*http.Response, error) {
    r.mu.Lock()
    if r.client == nil {
        r.client = r.owner.createClient(r.ip)
    }
    c := r.client
    r.mu.Unlock()

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

