package galactus

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"sync"
	"time"
)

type DatabaseError struct {
	Code, Message string
	Metadata      map[string]any
}

func (e *DatabaseError) Error() string { return e.Code + ": " + e.Message }

type Result struct {
	Keys    []string
	Records []map[string]any
	Summary map[string]any
}
type Driver struct {
	conn        net.Conn
	database    string
	timeout     time.Duration
	transaction bool
	mu          sync.Mutex
}

// Connect opens one serial connection. Timeout bounds each complete operation.
func Connect(uri, username, password, database string, timeout time.Duration) (*Driver, error) {
	u, e := url.Parse(uri)
	if e != nil {
		return nil, e
	}
	if (u.Scheme != "bolt" && u.Scheme != "bolt+s") || u.Hostname() == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("expected bolt://host:port or bolt+s://host:port")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if database == "" {
		database = "neo4j"
	}
	port := u.Port()
	if port == "" {
		port = "7687"
	}
	dial := net.Dialer{Timeout: timeout}
	var c net.Conn
	if u.Scheme == "bolt+s" {
		c, e = tls.DialWithDialer(&dial, "tcp", net.JoinHostPort(u.Hostname(), port), &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
	} else {
		c, e = dial.Dial("tcp", net.JoinHostPort(u.Hostname(), port))
	}
	if e != nil {
		return nil, e
	}
	d := &Driver{conn: c, database: database, timeout: timeout}
	c.SetDeadline(time.Now().Add(timeout))
	fail := func(err error) (*Driver, error) { c.Close(); return nil, err }
	if e = d.write([]byte{0x60, 0x60, 0xb0, 0x17, 0, 0, 4, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}); e != nil {
		return fail(e)
	}
	b := make([]byte, 4)
	if _, e = io.ReadFull(c, b); e != nil {
		return fail(e)
	}
	if binary.BigEndian.Uint32(b) != 0x404 {
		return fail(fmt.Errorf("server did not select Bolt 4.4"))
	}
	if e = d.send(1, map[string]any{"user_agent": "galactus-go/0.1", "scheme": "basic", "principal": username, "credentials": password}); e != nil {
		return fail(e)
	}
	if _, e = d.success(); e != nil {
		return fail(e)
	}
	return d, nil
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
func (d *Driver) send(tag byte, fields ...any) error {
	b, e := Encode(Structure{tag, fields})
	if e != nil {
		return e
	}
	if len(b) > MaxMessage {
		return fmt.Errorf("message exceeds 64 MiB")
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
	if !ok || len(m.Fields) != 1 {
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
	v, ok := m.Fields[0].(map[string]any)
	if m.Tag != 0x70 || !ok {
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
func (d *Driver) ExecuteQuery(query string, params map[string]any) (result Result, err error) {
	if err = validateParameters(params, 0); err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	defer func() {
		if err != nil {
			d.discard()
		}
	}()
	if err = d.start(); err != nil {
		return
	}
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
			result.Summary = meta
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
	defer func() {
		if err != nil {
			d.discard()
		}
	}()
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
	if err != nil {
		return
	}
	meta, err = d.success()
	if err == nil {
		d.transaction = tag == 0x11
	}
	return
}
func (d *Driver) Begin(readOnly bool) error       { _, e := d.control(0x11, readOnly); return e }
func (d *Driver) Commit() (map[string]any, error) { return d.control(0x12, false) }
func (d *Driver) Rollback() error                 { _, e := d.control(0x13, false); return e }
func (d *Driver) Close() error                    { d.mu.Lock(); defer d.mu.Unlock(); d.discard(); return nil }
