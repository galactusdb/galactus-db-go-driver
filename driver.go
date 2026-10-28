package galactus

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Version is the driver release.
const Version = "0.2.0"

// UserAgent is sent as user_agent and as the bolt_agent product.
const UserAgent = "galactus-db-go-driver/" + Version

// Options holds optional connection settings.
type Options struct {
	// TrustedCertificates is a path to one or more PEM certificates that
	// replace the platform trust store for a bolt+s:// connection. Setting it
	// on a bolt:// URI is a ConfigError. There is no option to skip
	// certificate verification.
	TrustedCertificates string
}

type Result struct {
	Keys    []string
	Records []map[string]any
	Summary map[string]any
}

type Driver struct {
	conn         net.Conn
	database     string
	timeout      time.Duration
	transaction  bool
	mu           sync.Mutex
	protocol     string
	bolt5        bool
	serverAgent  string
	connectionID string
	lastBookmark string
}

// Connect opens one serial connection. Timeout bounds each complete operation.
func Connect(uri, username, password, database string, timeout time.Duration) (*Driver, error) {
	return ConnectWithOptions(uri, username, password, database, timeout, Options{})
}

// ConnectWithOptions is Connect with optional settings such as a trusted
// certificate file for bolt+s://.
func ConnectWithOptions(uri, username, password, database string, timeout time.Duration, options Options) (*Driver, error) {
	u, e := url.Parse(uri)
	if e != nil {
		return nil, &ConfigError{"invalid URI: " + e.Error()}
	}
	if (u.Scheme != "bolt" && u.Scheme != "bolt+s") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, &ConfigError{"expected bolt://host:port or bolt+s://host:port"}
	}
	var tlsConfig *tls.Config
	if u.Scheme == "bolt+s" {
		tlsConfig = &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
		if options.TrustedCertificates != "" {
			pem, e := os.ReadFile(options.TrustedCertificates)
			if e != nil {
				return nil, &ConfigError{"cannot read trusted certificates: " + e.Error()}
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, &ConfigError{"trusted certificates file holds no PEM certificates"}
			}
			tlsConfig.RootCAs = pool
		}
	} else if options.TrustedCertificates != "" {
		return nil, &ConfigError{"trusted certificates require a bolt+s:// URI"}
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	port := u.Port()
	if port == "" {
		port = "7687"
	}
	dial := net.Dialer{Timeout: timeout}
	address := net.JoinHostPort(u.Hostname(), port)
	var c net.Conn
	if tlsConfig != nil {
		c, e = tls.DialWithDialer(&dial, "tcp", address, tlsConfig)
	} else {
		c, e = dial.Dial("tcp", address)
	}
	if e != nil {
		return nil, e
	}
	d := &Driver{conn: c, database: database, timeout: timeout}
	c.SetDeadline(time.Now().Add(timeout))
	if e = d.open(username, password); e != nil {
		c.Close()
		return nil, e
	}
	return d, nil
}

// open performs the handshake and authentication.
func (d *Driver) open(username, password string) error {
	// Preamble, then 5.4 accepting down to 5.0, 4.4, and two empty proposals.
	if e := d.write([]byte{0x60, 0x60, 0xb0, 0x17, 0, 4, 4, 5, 0, 0, 4, 4, 0, 0, 0, 0, 0, 0, 0, 0}); e != nil {
		return e
	}
	b := make([]byte, 4)
	if _, e := io.ReadFull(d.conn, b); e != nil {
		return e
	}
	switch {
	case b[0] == 0 && b[1] == 0 && b[3] == 5 && b[2] <= 4:
		d.protocol, d.bolt5 = "5."+strconv.Itoa(int(b[2])), true
	case b[0] == 0 && b[1] == 0 && b[2] == 4 && b[3] == 4:
		d.protocol = "4.4"
	default:
		return fmt.Errorf("server selected unsupported Bolt version %x", b)
	}
	logon := d.bolt5 && d.protocol != "5.0"
	hello := map[string]any{"user_agent": UserAgent}
	auth := map[string]any{"scheme": "basic", "principal": username, "credentials": password}
	if logon {
		hello["bolt_agent"] = map[string]any{
			"product":          UserAgent,
			"language":         "Go/" + runtime.Version(),
			"language_details": runtime.Compiler,
			"platform":         runtime.GOOS + "/" + runtime.GOARCH,
		}
	} else {
		for k, v := range auth {
			hello[k] = v
		}
	}
	if e := d.send(0x01, hello); e != nil {
		return e
	}
	meta, e := d.success()
	if e != nil {
		return e
	}
	d.serverAgent, _ = meta["server"].(string)
	d.connectionID, _ = meta["connection_id"].(string)
	if logon {
		if e = d.send(0x6a, auth); e != nil {
			return e
		}
		if _, e = d.success(); e != nil {
			return e
		}
	}
	return nil
}

// ProtocolVersion is the agreed Bolt version, "5.4" or "4.4" for example.
func (d *Driver) ProtocolVersion() string { return d.protocol }

// ServerAgent is the server field of the HELLO SUCCESS.
func (d *Driver) ServerAgent() string { return d.serverAgent }

// ConnectionID is the connection_id field of the HELLO SUCCESS.
func (d *Driver) ConnectionID() string { return d.connectionID }

// LastBookmark is the most recent bookmark from an autocommit query or a
// COMMIT. The driver does not send bookmarks.
func (d *Driver) LastBookmark() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastBookmark
}

func (d *Driver) write(b []byte) error {
	for len(b) > 0 {
		n, e := d.conn.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		b = b[n:]
	}
	return nil
}

// send encodes the complete message before writing; an encoding failure is
// returned as *encodeError with nothing written.
func (d *Driver) send(tag byte, fields ...any) error {
	if tag == 0x10 || tag == 0x11 {
		index := 0
		if tag == 0x10 {
			index = 2
		}
		extra := fields[index].(map[string]any)
		if extra["db"] == "" {
			delete(extra, "db")
		}
	}
	b, e := EncodeProtocol(Structure{tag, fields}, d.bolt5)
	if e != nil {
		return &encodeError{e}
	}
	if len(b) > MaxMessage {
		return &encodeError{fmt.Errorf("message exceeds 64 MiB")}
	}
	out := []byte{}
	for len(b) > 0 {
		n := len(b)
		if n > 65535 {
			n = 65535
		}
		out = binary.BigEndian.AppendUint16(out, uint16(n))
		out = append(out, b[:n]...)
		b = b[n:]
	}
	out = append(out, 0, 0)
	return d.write(out)
}

// receive reads one response. A FAILURE is returned as *DatabaseError.
func (d *Driver) receive() (Structure, error) {
	body := []byte{}
	h := make([]byte, 2)
	for {
		if _, e := io.ReadFull(d.conn, h); e != nil {
			return Structure{}, e
		}
		n := int(binary.BigEndian.Uint16(h))
		if n == 0 {
			if len(body) > 0 {
				break
			}
			continue
		}
		if len(body)+n > MaxMessage {
			return Structure{}, fmt.Errorf("message exceeds 64 MiB")
		}
		b := make([]byte, n)
		if _, e := io.ReadFull(d.conn, b); e != nil {
			return Structure{}, e
		}
		body = append(body, b...)
	}
	v, e := Decode(body)
	if e != nil {
		return Structure{}, e
	}
	m, ok := v.(Structure)
	if !ok {
		return m, fmt.Errorf("invalid Bolt response")
	}
	if m.Tag == 0x7e && len(m.Fields) <= 1 {
		return m, nil
	}
	if len(m.Fields) != 1 {
		return m, fmt.Errorf("invalid Bolt response")
	}
	if m.Tag == 0x7f {
		meta, ok := m.Fields[0].(map[string]any)
		if !ok {
			return m, fmt.Errorf("invalid failure")
		}
		code, _ := meta["code"].(string)
		msg, _ := meta["message"].(string)
		return m, &DatabaseError{code, msg, meta}
	}
	if m.Tag != 0x70 && m.Tag != 0x71 {
		return m, fmt.Errorf("unexpected Bolt response")
	}
	return m, nil
}
func (d *Driver) success() (map[string]any, error) {
	m, e := d.receive()
	if e != nil {
		return nil, e
	}
	if m.Tag != 0x70 {
		return nil, fmt.Errorf("expected SUCCESS")
	}
	v, ok := m.Fields[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected SUCCESS")
	}
	return v, nil
}
func (d *Driver) start() error {
	if d.conn == nil {
		return fmt.Errorf("driver is closed")
	}
	return d.conn.SetDeadline(time.Now().Add(d.timeout))
}
func (d *Driver) discard() {
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
	d.transaction = false
}

// reset sends RESET and reads until its SUCCESS, skipping IGNORED.
func (d *Driver) reset() error {
	if e := d.start(); e != nil {
		return e
	}
	if e := d.send(0x0f); e != nil {
		return e
	}
	for {
		m, e := d.receive()
		if e != nil {
			return e
		}
		switch m.Tag {
		case 0x70:
			return nil
		case 0x7e:
			continue
		default:
			return fmt.Errorf("unexpected response to RESET")
		}
	}
}

// finish applies the failure rules: a database FAILURE resets the connection
// (the server rolls back any open transaction) and keeps it; an encoding
// failure leaves it untouched; anything else discards it.
func (d *Driver) finish(err error) error {
	if err == nil {
		return nil
	}
	var dbe *DatabaseError
	if errors.As(err, &dbe) {
		d.transaction = false
		if d.reset() != nil {
			d.discard()
		}
		return err
	}
	var ee *encodeError
	if errors.As(err, &ee) {
		return ee.err
	}
	d.discard()
	return err
}

// ExecuteQuery runs one query, in the open transaction if there is one, and
// buffers its records. A *DatabaseError leaves the driver usable; an open
// transaction is rolled back by the server in that case.
func (d *Driver) ExecuteQuery(query string, params map[string]any) (result Result, err error) {
	if err = validateParameters(params, 0); err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err = d.start(); err != nil {
		return
	}
	result, err = d.run(query, params)
	err = d.finish(err)
	return
}

func (d *Driver) run(query string, params map[string]any) (result Result, err error) {
	if params == nil {
		params = map[string]any{}
	}
	extra := map[string]any{}
	if !d.transaction {
		extra["db"] = d.database
	}
	if err = d.send(0x10, query, params, extra); err != nil {
		return
	}
	var meta map[string]any
	meta, err = d.success()
	if err != nil {
		return
	}
	fields, ok := meta["fields"].([]any)
	if !ok {
		err = fmt.Errorf("missing fields")
		return
	}
	for _, f := range fields {
		k, ok := f.(string)
		if !ok {
			err = fmt.Errorf("invalid field name")
			return
		}
		result.Keys = append(result.Keys, k)
	}
	result.Records = []map[string]any{}
	if err = d.send(0x3f, map[string]any{"n": int64(-1)}); err != nil {
		return
	}
	for {
		var m Structure
		m, err = d.receive()
		if err != nil {
			return
		}
		if m.Tag == 0x70 {
			summary, ok := m.Fields[0].(map[string]any)
			if !ok {
				err = fmt.Errorf("invalid summary")
				return
			}
			if summary["has_more"] == true {
				if err = d.send(0x3f, map[string]any{"n": int64(-1)}); err != nil {
					return
				}
				continue
			}
			for k, v := range summary {
				meta[k] = v
			}
			if b, ok := summary["bookmark"].(string); ok && b != "" {
				d.lastBookmark = b
			}
			result.Summary = meta
			return
		}
		if m.Tag != 0x71 {
			err = fmt.Errorf("unexpected Bolt response")
			return
		}
		values, ok := m.Fields[0].([]any)
		if !ok || len(values) != len(result.Keys) {
			err = fmt.Errorf("record width mismatch")
			return
		}
		row := map[string]any{}
		for i, k := range result.Keys {
			row[k] = values[i]
		}
		result.Records = append(result.Records, row)
	}
}
func (d *Driver) control(tag byte, readOnly bool) (meta map[string]any, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if tag == 0x11 && d.transaction {
		return nil, fmt.Errorf("transaction already open")
	}
	if tag != 0x11 && !d.transaction {
		return nil, fmt.Errorf("no transaction")
	}
	if err = d.start(); err != nil {
		return
	}
	if tag == 0x11 {
		mode := "w"
		if readOnly {
			mode = "r"
		}
		err = d.send(tag, map[string]any{"db": d.database, "mode": mode})
	} else {
		err = d.send(tag)
	}
	if err == nil {
		meta, err = d.success()
	}
	if err != nil {
		return nil, d.finish(err)
	}
	d.transaction = tag == 0x11
	if b, ok := meta["bookmark"].(string); ok && tag == 0x12 && b != "" {
		d.lastBookmark = b
	}
	return
}

// Begin opens an explicit transaction on this connection.
func (d *Driver) Begin(readOnly bool) error { _, e := d.control(0x11, readOnly); return e }

// Commit commits the open transaction. A *DatabaseError means it did not
// commit; any other error during COMMIT leaves the outcome unknown.
func (d *Driver) Commit() (map[string]any, error) { return d.control(0x12, false) }
func (d *Driver) Rollback() error                 { _, e := d.control(0x13, false); return e }
func (d *Driver) Close() error                    { d.mu.Lock(); defer d.mu.Unlock(); d.discard(); return nil }
