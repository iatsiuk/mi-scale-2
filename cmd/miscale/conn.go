package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"miscale/internal/ble"
	"miscale/internal/scale"
	"miscale/internal/store"
)

var serviceData = ble.NormalizeUUID("181b")

// bleGATT adapts ble.Device to scale.GATT.
type bleGATT struct{ d *ble.Device }

func (g bleGATT) Read(ctx context.Context, service, char string) ([]byte, error) {
	c, err := g.d.Char(service, char)
	if err != nil {
		return nil, err
	}
	return g.d.Read(ctx, c)
}

func (g bleGATT) Write(ctx context.Context, service, char string, data []byte) error {
	c, err := g.d.Char(service, char)
	if err != nil {
		return err
	}
	return g.d.Write(ctx, c, data, true)
}

func (g bleGATT) Subscribe(ctx context.Context, service, char string) (<-chan []byte, error) {
	c, err := g.d.Char(service, char)
	if err != nil {
		return nil, err
	}
	return g.d.Subscribe(ctx, c)
}

func (g bleGATT) Unsubscribe(ctx context.Context, service, char string) error {
	c, err := g.d.Char(service, char)
	if err != nil {
		return err
	}
	return g.d.Unsubscribe(ctx, c)
}

func openAdapter(ctx context.Context) (*ble.Adapter, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	a, err := ble.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w (is Bluetooth on and allowed for the terminal?)", err)
	}
	return a, nil
}

type advert struct {
	id string
	m  scale.Measurement
}

// advertisements streams parsed scale advertisements; with id set only that
// peripheral is reported. The channel closes when the scan has ended.
func advertisements(ctx context.Context, a *ble.Adapter, id string) <-chan advert {
	out := make(chan advert, 64)
	go func() {
		defer close(out)
		err := a.Scan(ctx, func(adv ble.Advertisement) {
			data, ok := adv.ServiceData[serviceData]
			if !ok || (id != "" && adv.ID != id) {
				return
			}
			m, err := scale.ParseMeasurement(data)
			if err != nil {
				return
			}
			select {
			case out <- advert{id: adv.ID, m: m}:
			default:
			}
		})
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
		}
	}()
	return out
}

// stopScan cancels a scan from advertisements and waits until it has ended.
func stopScan(cancel context.CancelFunc, ch <-chan advert) {
	cancel()
	for range ch {
	}
}

func waitScale(ctx context.Context, a *ble.Adapter, id string) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	ch := advertisements(ctx, a, id)
	defer stopScan(cancel, ch)
	for adv := range ch {
		return adv.id, nil
	}
	return "", errors.New("scale not found: step on the scale to wake it up")
}

// connection is an initialized GATT session with the scale.
type connection struct {
	dev  *ble.Device
	sess *scale.Session
	info scale.DeviceInfo
}

func (c *connection) Close() { c.dev.Close() }

// connect wakes, connects and initializes the scale like the app does on
// every connection: user mode check, UTC clock sync, device info.
func connect(ctx context.Context, e *env) (*connection, error) {
	a, err := openAdapter(ctx)
	if err != nil {
		return nil, err
	}
	e.printf("waiting for the scale, step on it to wake it up...\n")
	id, err := waitScale(ctx, a, e.data.Scale.ID)
	if err != nil {
		return nil, err
	}
	dev, err := a.Connect(ctx, id)
	if err != nil {
		return nil, err
	}
	c := &connection{dev: dev, sess: scale.NewSession(bleGATT{dev})}
	if err := c.init(ctx); err != nil {
		dev.Close()
		return nil, err
	}
	e.data.Scale = store.Scale{ID: id, Serial: c.info.Serial}
	watchBattery(ctx, e, dev)
	return c, nil
}

func (c *connection) init(ctx context.Context) error {
	if err := c.sess.Init(ctx); err != nil {
		return err
	}
	info, err := c.sess.DeviceInfo(ctx)
	if err != nil {
		return err
	}
	if info.Model == scale.ModelUnknown {
		return fmt.Errorf("unsupported scale: PnP product id %d", info.PnP.ProductID)
	}
	c.info = info
	return nil
}

// watchBattery reports the low battery notification (0x1543 `01 01`).
func watchBattery(ctx context.Context, e *env, dev *ble.Device) {
	ch, err := bleGATT{dev}.Subscribe(ctx, scale.ServiceHuami, scale.CharStatus)
	if err != nil {
		return
	}
	go func() {
		for b := range ch {
			if len(b) == 2 && b[0] == 1 && b[1] == 1 {
				e.printf("warning: low battery\n")
			}
		}
	}()
}
