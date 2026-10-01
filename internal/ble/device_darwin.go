package ble

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/tinygo-org/cbgo"
)

// Characteristic is a discovered GATT characteristic.
type Characteristic struct {
	Service string
	UUID    string
	chr     cbgo.Characteristic
}

func (c *Characteristic) CanRead() bool {
	return c.chr.Properties()&cbgo.CharacteristicPropertyRead != 0
}

// PropsString renders properties as r/w/W/n/i (read, write, write without
// response, notify, indicate).
func (c *Characteristic) PropsString() string {
	p := c.chr.Properties()
	flags := []struct {
		bit cbgo.CharacteristicProperties
		ch  byte
	}{
		{cbgo.CharacteristicPropertyRead, 'r'},
		{cbgo.CharacteristicPropertyWrite, 'w'},
		{cbgo.CharacteristicPropertyWriteWithoutResponse, 'W'},
		{cbgo.CharacteristicPropertyNotify, 'n'},
		{cbgo.CharacteristicPropertyIndicate, 'i'},
	}
	var out []byte
	for _, f := range flags {
		if p&f.bit != 0 {
			out = append(out, f.ch)
		}
	}
	return string(out)
}

// Device is a connected peripheral. Operations are serialized: one GATT
// request is in flight at a time.
type Device struct {
	cbgo.PeripheralDelegateBase

	ID string

	a *Adapter
	p cbgo.Peripheral

	op sync.Mutex // serializes gatt requests

	mu      sync.Mutex
	chars   map[string]*Characteristic // key: service + "/" + char
	pending map[pendingKey]chan result
	subs    map[cbgo.Characteristic]chan []byte
	done    chan struct{}
	doneErr error
	closed  bool
}

type pendingKind int

const (
	kindServices pendingKind = iota
	kindChars
	kindRead
	kindWrite
	kindNotify
)

type pendingKey struct {
	kind pendingKind
	ptr  any // cbgo.Service or cbgo.Characteristic; nil for services
}

type result struct {
	data []byte
	err  error
}

func newDevice(a *Adapter, p cbgo.Peripheral) *Device {
	return &Device{
		ID:      p.Identifier().String(),
		a:       a,
		p:       p,
		chars:   map[string]*Characteristic{},
		pending: map[pendingKey]chan result{},
		subs:    map[cbgo.Characteristic]chan []byte{},
		done:    make(chan struct{}),
	}
}

// Done is closed when the device disconnects.
func (d *Device) Done() <-chan struct{} { return d.done }

// Close disconnects from the peripheral.
func (d *Device) Close() {
	d.a.cm.CancelConnect(d.p)
	d.disconnected(errors.New("ble: closed"))
}

func (d *Device) disconnected(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	d.doneErr = err
	for k, ch := range d.pending {
		ch <- result{err: err}
		delete(d.pending, k)
	}
	for k, ch := range d.subs {
		close(ch)
		delete(d.subs, k)
	}
	close(d.done)
}

// request registers a pending callback, runs start and waits for the result.
func (d *Device) request(ctx context.Context, key pendingKey, start func()) ([]byte, error) {
	ch := make(chan result, 1)
	d.mu.Lock()
	if d.closed {
		err := d.doneErr
		d.mu.Unlock()
		return nil, err
	}
	d.pending[key] = ch
	d.mu.Unlock()

	start()
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		// the operation stays in flight and a late callback could complete
		// the next request with the same key, so the link is unusable now
		d.mu.Lock()
		delete(d.pending, key)
		d.mu.Unlock()
		err := fmt.Errorf("ble: gatt operation abandoned, connection closed: %w", ctx.Err())
		d.a.cm.CancelConnect(d.p)
		d.disconnected(err)
		return nil, err
	}
}

func (d *Device) complete(key pendingKey, r result) bool {
	d.mu.Lock()
	ch, ok := d.pending[key]
	delete(d.pending, key)
	d.mu.Unlock()
	if ok {
		ch <- r
	}
	return ok
}

func (d *Device) discover(ctx context.Context) error {
	d.op.Lock()
	defer d.op.Unlock()
	if _, err := d.request(ctx, pendingKey{kind: kindServices}, func() { d.p.DiscoverServices(nil) }); err != nil {
		return fmt.Errorf("ble: discover services: %w", err)
	}
	for _, svc := range d.p.Services() {
		if _, err := d.request(ctx, pendingKey{kindChars, svc}, func() { d.p.DiscoverCharacteristics(nil, svc) }); err != nil {
			return fmt.Errorf("ble: discover characteristics of %s: %w", svc.UUID(), err)
		}
		su := NormalizeUUID(svc.UUID().String())
		for _, c := range svc.Characteristics() {
			cu := NormalizeUUID(c.UUID().String())
			d.mu.Lock()
			d.chars[su+"/"+cu] = &Characteristic{Service: su, UUID: cu, chr: c}
			d.mu.Unlock()
		}
	}
	return nil
}

// Characteristics lists everything discovered on connect.
func (d *Device) Characteristics() []*Characteristic {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*Characteristic, 0, len(d.chars))
	for _, c := range d.chars {
		out = append(out, c)
	}
	return out
}

// Char returns the characteristic or ErrNotFound.
func (d *Device) Char(service, char string) (*Characteristic, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.chars[NormalizeUUID(service)+"/"+NormalizeUUID(char)]
	if !ok {
		return nil, fmt.Errorf("ble: characteristic %s/%s: %w", service, char, ErrNotFound)
	}
	return c, nil
}

func (d *Device) Read(ctx context.Context, c *Characteristic) ([]byte, error) {
	d.op.Lock()
	defer d.op.Unlock()
	return d.request(ctx, pendingKey{kindRead, c.chr}, func() { d.p.ReadCharacteristic(c.chr) })
}

func (d *Device) Write(ctx context.Context, c *Characteristic, data []byte, withResponse bool) error {
	d.op.Lock()
	defer d.op.Unlock()
	if !withResponse {
		d.p.WriteCharacteristic(data, c.chr, false)
		return nil
	}
	_, err := d.request(ctx, pendingKey{kindWrite, c.chr}, func() { d.p.WriteCharacteristic(data, c.chr, true) })
	return err
}

// Subscribe enables notifications/indications. The channel is closed on
// disconnect or Unsubscribe.
func (d *Device) Subscribe(ctx context.Context, c *Characteristic) (<-chan []byte, error) {
	ch := make(chan []byte, 256)
	d.mu.Lock()
	if old, ok := d.subs[c.chr]; ok {
		close(old)
	}
	d.subs[c.chr] = ch
	d.mu.Unlock()

	d.op.Lock()
	defer d.op.Unlock()
	if _, err := d.request(ctx, pendingKey{kindNotify, c.chr}, func() { d.p.SetNotify(true, c.chr) }); err != nil {
		d.mu.Lock()
		if d.subs[c.chr] == ch {
			delete(d.subs, c.chr)
			close(ch)
		}
		d.mu.Unlock()
		return nil, fmt.Errorf("ble: subscribe %s: %w", c.UUID, err)
	}
	return ch, nil
}

func (d *Device) Unsubscribe(ctx context.Context, c *Characteristic) error {
	d.mu.Lock()
	if ch, ok := d.subs[c.chr]; ok {
		delete(d.subs, c.chr)
		close(ch)
	}
	d.mu.Unlock()

	d.op.Lock()
	defer d.op.Unlock()
	_, err := d.request(ctx, pendingKey{kindNotify, c.chr}, func() { d.p.SetNotify(false, c.chr) })
	return err
}

func (d *Device) DidDiscoverServices(_ cbgo.Peripheral, err error) {
	d.complete(pendingKey{kind: kindServices}, result{err: err})
}

func (d *Device) DidDiscoverCharacteristics(_ cbgo.Peripheral, svc cbgo.Service, err error) {
	d.complete(pendingKey{kindChars, svc}, result{err: err})
}

func (d *Device) DidUpdateValueForCharacteristic(_ cbgo.Peripheral, c cbgo.Characteristic, err error) {
	var data []byte
	if err == nil {
		data = append([]byte(nil), c.Value()...)
	}
	if d.complete(pendingKey{kindRead, c}, result{data: data, err: err}) || err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ch, ok := d.subs[c]; ok {
		select {
		case ch <- data:
		default: // consumer is stuck; drop rather than block the bluetooth queue
		}
	}
}

func (d *Device) DidWriteValueForCharacteristic(_ cbgo.Peripheral, c cbgo.Characteristic, err error) {
	d.complete(pendingKey{kindWrite, c}, result{err: err})
}

func (d *Device) DidUpdateNotificationState(_ cbgo.Peripheral, c cbgo.Characteristic, err error) {
	d.complete(pendingKey{kindNotify, c}, result{err: err})
}
