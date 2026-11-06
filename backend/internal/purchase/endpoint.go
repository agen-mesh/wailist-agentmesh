package purchase

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/agentmesh/backend/internal/netutil"
)

type EndpointPolicy struct {
	resolver *net.Resolver
	dialer   *net.Dialer
}

func NewEndpointPolicy() *EndpointPolicy {
	return &EndpointPolicy{resolver: net.DefaultResolver, dialer: &net.Dialer{Timeout: 5 * time.Second}}
}

func endpointURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || u.ForceQuery || (u.Port() != "" && u.Port() != "443") {
		return nil, fmt.Errorf("merchant endpoint must be a public HTTPS URL without credentials or query")
	}
	return u, nil
}
func (p *EndpointPolicy) addresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, err := p.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("merchant hostname could not be resolved")
	}
	for _, ip := range ips {
		if netutil.IsPrivateIP(ip.IP) || !ip.IP.IsGlobalUnicast() {
			return nil, fmt.Errorf("merchant endpoint cannot address a private or reserved network")
		}
	}
	return ips, nil
}
func (p *EndpointPolicy) Validate(ctx context.Context, raw string) error {
	u, err := endpointURL(raw)
	if err != nil {
		return err
	}
	_, err = p.addresses(ctx, u.Hostname())
	return err
}
func (p *EndpointPolicy) Client() *http.Client {
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, fmt.Errorf("merchant port rejected")
		}
		ips, err := p.addresses(ctx, host)
		if err != nil {
			return nil, err
		}
		return p.dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("merchant redirects are not permitted") }}
}
