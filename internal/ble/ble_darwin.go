// Package ble is a small synchronous wrapper over CoreBluetooth (via cbgo).
package ble

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/tinygo-org/cbgo"
)

var ErrNotFound = errors.New("ble: not found")

// Advertisement is a single scan result.
type Advertisement struct {
	ID          string
	Name        string
	RSSI        int
	ServiceData map[string][]byte // key: normalized 128-bit service uuid
}

// Adapter owns the CoreBluetooth central manager.
type Adapter struct {
	cbgo.CentralManagerDelegateBase

	cm cbgo.CentralManager

	mu          sync.Mutex
	powered     chan struct{}
	poweredOnce sync.Once
	connects    map[string]chan error
	devices     map[string]*Device

	// scanMu is held for reading while a scan callback runs, so Scan can
	// wait for in-flight callbacks before it returns.
	scanMu sync.RWMutex
	onAdv  func(Advertisement)
}

// Open initializes CoreBluetooth and waits until the radio is powered on.
func Open(ctx context.Context) (*Adapter, error) {
	a := &Adapter{
		powered:  make(chan struct{}),
		connects: map[string]chan error{},
		devices:  map[string]*Device{},
	}
	a.cm = cbgo.NewCentralManager(nil)
	a.cm.SetDelegate(a)
	select {
	case <-a.powered:
		return a, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("ble: bluetooth not powered on (state %d): %w", a.cm.State(), ctx.Err())
	}
}

func (a *Adapter) CentralManagerDidUpdateState(cm cbgo.CentralManager) {
	if cm.State() == cbgo.ManagerStatePoweredOn {
		a.poweredOnce.Do(func() { close(a.powered) })
	}
}

//nolint:gocritic // signature is fixed by cbgo.CentralManagerDelegate
func (a *Adapter) DidDiscoverPeripheral(_ cbgo.CentralManager, p cbgo.Peripheral, f cbgo.AdvFields, rssi int) {
	a.scanMu.RLock()
	defer a.scanMu.RUnlock()
	cb := a.onAdv
	if cb == nil {
		return
	}
	adv := Advertisement{ID: p.Identifier().String(), Name: f.LocalName, RSSI: rssi, ServiceData: map[string][]byte{}}
	if adv.Name == "" {
		adv.Name = p.Name()
	}
	for _, sd := range f.ServiceData {
		adv.ServiceData[NormalizeUUID(sd.UUID.String())] = append([]byte(nil), sd.Data...)
	}
	cb(adv)
}

// Scan reports every advertisement (duplicates included) until ctx is done.
// The callback runs on the CoreBluetooth queue and must not block; no
// callback runs after Scan returns.
func (a *Adapter) Scan(ctx context.Context, cb func(Advertisement)) error {
	a.scanMu.Lock()
	if a.onAdv != nil {
		a.scanMu.Unlock()
		return errors.New("ble: scan already running")
	}
	a.onAdv = cb
	a.scanMu.Unlock()

	a.cm.Scan(nil, &cbgo.CentralManagerScanOpts{AllowDuplicates: true})
	<-ctx.Done()
	a.cm.StopScan()

	a.scanMu.Lock() // waits for a callback in flight
	a.onAdv = nil
	a.scanMu.Unlock()
	return nil
}

func (a *Adapter) DidConnectPeripheral(_ cbgo.CentralManager, p cbgo.Peripheral) {
	a.finishConnect(p.Identifier().String(), nil)
}

func (a *Adapter) DidFailToConnectPeripheral(_ cbgo.CentralManager, p cbgo.Peripheral, err error) {
	if err == nil {
		err = errors.New("ble: connect failed")
	}
	a.finishConnect(p.Identifier().String(), err)
}

func (a *Adapter) DidDisconnectPeripheral(_ cbgo.CentralManager, p cbgo.Peripheral, err error) {
	id := p.Identifier().String()
	if err == nil {
		err = errors.New("ble: disconnected")
	}
	a.finishConnect(id, err)
	a.mu.Lock()
	d := a.devices[id]
	delete(a.devices, id)
	a.mu.Unlock()
	if d != nil {
		d.disconnected(err)
	}
}

func (a *Adapter) finishConnect(id string, err error) {
	a.mu.Lock()
	ch := a.connects[id]
	delete(a.connects, id)
	a.mu.Unlock()
	if ch != nil {
		ch <- err
	}
}

// Connect connects to a peripheral seen in a scan and discovers all services
// and characteristics.
func (a *Adapter) Connect(ctx context.Context, id string) (*Device, error) {
	uuid, err := cbgo.ParseUUID(id)
	if err != nil {
		return nil, err
	}
	prphs := a.cm.RetrievePeripheralsWithIdentifiers([]cbgo.UUID{uuid})
	if len(prphs) == 0 {
		return nil, fmt.Errorf("ble: peripheral %s: %w", id, ErrNotFound)
	}
	p := prphs[0]
	d := newDevice(a, p)
	p.SetDelegate(d)

	ch := make(chan error, 1)
	a.mu.Lock()
	a.connects[id] = ch
	a.devices[id] = d
	a.mu.Unlock()

	a.cm.Connect(p, nil)
	select {
	case err = <-ch:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil {
		a.mu.Lock()
		delete(a.connects, id)
		delete(a.devices, id)
		a.mu.Unlock()
		a.cm.CancelConnect(p)
		return nil, fmt.Errorf("ble: connect %s: %w", id, err)
	}
	if err := d.discover(ctx); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

// NormalizeUUID expands 16/32-bit uuids to the lowercase 128-bit form.
func NormalizeUUID(s string) string {
	s = strings.ToLower(s)
	switch len(s) {
	case 4:
		return "0000" + s + "-0000-1000-8000-00805f9b34fb"
	case 8:
		return s + "-0000-1000-8000-00805f9b34fb"
	}
	return s
}
