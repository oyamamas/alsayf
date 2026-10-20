package winrm

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	upstream "github.com/masterzen/winrm"
	"github.com/masterzen/winrm/soap"
)

// The upstream transport creates requests with context.Background. Keep its
// NTLM negotiation and message sealing, but bind sockets to our session context
// and bound reads as well as response headers. This also cancels CreateShell.
type transport struct {
	base    upstream.Transporter
	timeout time.Duration
	mu      sync.Mutex
	ctx     context.Context
	conns   map[*connection]struct{}
	stop    func()
}

func newTransport(ctx context.Context, auth string, timeout time.Duration) *transport {
	t := &transport{ctx: ctx, timeout: timeout, conns: make(map[*connection]struct{})}
	if auth == "ntlm" {
		t.base = upstream.NewClientNTLMWithDial(t.dial)
	} else {
		t.base = upstream.NewClientWithDial(t.dial)
	}
	t.watch(ctx)
	return t
}

func (t *transport) watch(ctx context.Context) {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		t.closeConnections()
		close(done)
	})
	t.stop = func() {
		if !stop() {
			<-done
		}
	}
}

func (t *transport) currentContext() context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ctx
}

func (t *transport) dial(network, address string) (net.Conn, error) {
	ctx := t.currentContext()
	d := net.Dialer{Timeout: t.timeout, KeepAlive: 30 * time.Second}
	c, err := d.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	tracked := &connection{Conn: c, owner: t}
	t.mu.Lock()
	if err := ctx.Err(); err != nil {
		t.mu.Unlock()
		c.Close()
		return nil, err
	}
	t.conns[tracked] = struct{}{}
	t.mu.Unlock()
	return tracked, nil
}

func (t *transport) Transport(endpoint *upstream.Endpoint) error {
	return t.base.Transport(endpoint)
}

func (t *transport) Post(client *upstream.Client, request *soap.SoapMessage) (string, error) {
	ctx := t.currentContext()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	response, err := t.base.Post(client, request)
	if ctx.Err() != nil {
		// Return the context error directly: upstream retries *url.Error timeouts
		// while polling output, which would otherwise loop after cancellation.
		return "", ctx.Err()
	}
	if err == nil {
		err = responseFault(response)
	}
	return response, err
}

// Upstream's output parser does not detect SOAP faults. Without this check an
// access-denied or invalid-command response can be mistaken for an idle poll.
func responseFault(response string) error {
	var envelope struct {
		XMLName xml.Name
		Header  struct{ Action string }
		Body    struct {
			Fault *struct {
				Reason struct{ Text string }
				Detail struct {
					WSManFault struct {
						Code string `xml:",attr"`
					}
				}
			}
		}
	}
	if err := xml.Unmarshal([]byte(response), &envelope); err != nil {
		return fmt.Errorf("invalid WinRM SOAP response: %w", err)
	}
	if envelope.XMLName.Local != "Envelope" || envelope.XMLName.Space != "http://www.w3.org/2003/05/soap-envelope" {
		return errors.New("WinRM response is not a SOAP 1.2 envelope")
	}
	fault := envelope.Body.Fault
	if fault == nil {
		if envelope.Header.Action == "" {
			return errors.New("WinRM response is missing its SOAP action")
		}
		var id string
		var err error
		switch envelope.Header.Action {
		case "http://schemas.xmlsoap.org/ws/2004/09/transfer/CreateResponse":
			id, err = upstream.ParseOpenShellResponse(response)
		case "http://schemas.microsoft.com/wbem/wsman/1/windows/shell/CommandResponse":
			id, err = upstream.ParseExecuteCommandResponse(response)
		default:
			return nil
		}
		if err != nil || id == "" {
			return errors.New("WinRM response is missing a valid shell or command ID")
		}
		return nil
	}
	if fault.Detail.WSManFault.Code == "2150858793" {
		return errors.New("OperationTimeout")
	}
	return fmt.Errorf("WinRM SOAP fault (code %s): %s", fault.Detail.WSManFault.Code, fault.Reason.Text)
}

// Cleanup may need a fresh authenticated connection after cancellation. Call
// only once the command and its output readers have stopped using the transport.
func (t *transport) cleanupContext() context.CancelFunc {
	t.stop()
	t.closeConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.mu.Lock()
	t.ctx = ctx
	t.mu.Unlock()
	t.watch(ctx)
	return cancel
}

func (t *transport) closeConnections() {
	t.mu.Lock()
	conns := make([]*connection, 0, len(t.conns))
	for c := range t.conns {
		conns = append(conns, c)
	}
	t.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

func (t *transport) close() {
	t.stop()
	t.closeConnections()
}

type connection struct {
	net.Conn
	owner *transport
}

func (c *connection) Read(p []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.owner.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

func (c *connection) Write(p []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.owner.timeout)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}

func (c *connection) Close() error {
	c.owner.mu.Lock()
	delete(c.owner.conns, c)
	c.owner.mu.Unlock()
	return c.Conn.Close()
}
