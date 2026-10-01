package scale

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoResponse        = errors.New("scale: no response")
	ErrIncompleteHistory = errors.New("scale: incomplete history transfer")
	ErrDisconnected      = errors.New("scale: disconnected")
)

// StatusError is a non-success answer to a control command.
type StatusError struct {
	Command byte
	Status  Status
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("scale: command %#02x: %s", e.Command, e.Status)
}

// GATT is the transport used by Session; writes are with response.
type GATT interface {
	Read(ctx context.Context, service, char string) ([]byte, error)
	Write(ctx context.Context, service, char string, data []byte) error
	Subscribe(ctx context.Context, service, char string) (<-chan []byte, error)
	Unsubscribe(ctx context.Context, service, char string) error
}

// Session runs scale procedures over a connected GATT link.
type Session struct {
	g GATT

	Now             func() time.Time
	ResponseTimeout time.Duration // app: 5 s
	HistoryIdle     time.Duration // app: 10 s

	mu   sync.Mutex
	subs map[string]<-chan []byte
}

func NewSession(g GATT) *Session {
	return &Session{
		g:               g,
		Now:             time.Now,
		ResponseTimeout: 5 * time.Second,
		HistoryIdle:     10 * time.Second,
		subs:            map[string]<-chan []byte{},
	}
}

// subscribe returns a cached subscription with stale notifications dropped.
func (s *Session) subscribe(ctx context.Context, service, char string) (<-chan []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.subs[char]; ok {
		if drain(ch) {
			return ch, nil
		}
		delete(s.subs, char)
		return nil, ErrDisconnected
	}
	ch, err := s.g.Subscribe(ctx, service, char)
	if err != nil {
		return nil, err
	}
	s.subs[char] = ch
	return ch, nil
}

// drain discards pending notifications; false means the channel is closed.
func drain(ch <-chan []byte) bool {
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return false
			}
		default:
			return true
		}
	}
}

// Init mirrors WeightBfsProfile init plus the time sync done on connect.
func (s *Session) Init(ctx context.Context) error {
	b, err := s.g.Read(ctx, ServiceHuami, CharControl)
	mode, perr := ParseMode(b)
	if err != nil || perr != nil || mode.NeedsUserMode() {
		if err := s.g.Write(ctx, ServiceHuami, CharControl, CmdUserMode()); err != nil {
			return fmt.Errorf("scale: set user mode: %w", err)
		}
	}
	return s.SyncTime(ctx)
}

func (s *Session) SyncTime(ctx context.Context) error {
	if err := s.g.Write(ctx, ServiceBodyComposition, CharCurrentTime, EncodeTime(s.Now())); err != nil {
		return fmt.Errorf("scale: set time: %w", err)
	}
	return nil
}

func (s *Session) Time(ctx context.Context) (time.Time, error) {
	b, err := s.g.Read(ctx, ServiceBodyComposition, CharCurrentTime)
	if err != nil {
		return time.Time{}, err
	}
	return DecodeTime(b)
}

type DeviceInfo struct {
	Model    Model
	PnP      PnP
	Software string
	Hardware string
	Serial   string
	SystemID string
}

func (s *Session) DeviceInfo(ctx context.Context) (DeviceInfo, error) {
	var info DeviceInfo
	str := func(char string) (string, error) {
		b, err := s.g.Read(ctx, ServiceDeviceInfo, char)
		return strings.TrimRight(string(b), "\x00"), err
	}
	var err error
	if info.Software, err = str(CharSoftwareRev); err != nil {
		return info, err
	}
	if info.Hardware, err = str(CharHardwareRev); err != nil {
		return info, err
	}
	if info.Serial, err = str(CharSerial); err != nil {
		return info, err
	}
	b, err := s.g.Read(ctx, ServiceDeviceInfo, CharSystemID)
	if err != nil {
		return info, err
	}
	info.SystemID = hex.EncodeToString(b)
	if b, err = s.g.Read(ctx, ServiceDeviceInfo, CharPnP); err != nil {
		return info, err
	}
	if info.PnP, err = ParsePnP(b); err != nil {
		return info, err
	}
	info.Model = info.PnP.Model()
	return info, nil
}

// command writes a control command and waits for its `10 06 cmd` answer.
func (s *Session) command(ctx context.Context, cmd []byte) (<-chan []byte, error) {
	ch, err := s.subscribe(ctx, ServiceHuami, CharControl)
	if err != nil {
		return nil, err
	}
	if err := s.g.Write(ctx, ServiceHuami, CharControl, cmd); err != nil {
		return nil, err
	}
	timer := time.NewTimer(s.ResponseTimeout)
	defer timer.Stop()
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return nil, ErrDisconnected
			}
			if done, err := answer(b, cmd); done {
				return ch, err
			}
		case <-timer.C:
			return nil, fmt.Errorf("command %x: %w", cmd, ErrNoResponse)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// answer reports whether b is the `10 group cmd 00 status` answer to cmd and
// the resulting error; other notifications on the channel are skipped.
func answer(b, cmd []byte) (bool, error) {
	if len(b) != 5 || b[0] != 0x10 || b[1] != cmd[0] || b[2] != cmd[1] {
		return false, nil
	}
	if st := Status(b[4]); st != StatusSuccess {
		return true, &StatusError{Command: cmd[1], Status: st}
	}
	return true, nil
}

// EraseHistory deletes all offline records stored on the scale.
func (s *Session) EraseHistory(ctx context.Context) error {
	_, err := s.command(ctx, CmdEraseHistory())
	return err
}

func (s *Session) SetPartMeasure(ctx context.Context, enable bool) error {
	_, err := s.command(ctx, CmdPartMeasure(enable))
	return err
}

// SetUnit changes the unit on the scale display; the scale does not answer.
func (s *Session) SetUnit(ctx context.Context, u Unit) error {
	return s.g.Write(ctx, ServiceHuami, CharControl, CmdSetUnit(u))
}

// OneFoot runs the one-foot balance test until the scale reports completion.
// Cancelling ctx stops the test on the scale.
func (s *Session) OneFoot(ctx context.Context, progress func(OneFoot)) error {
	ch, err := s.command(ctx, CmdOneFootStart())
	if err != nil {
		return err
	}
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return ErrDisconnected
			}
			p, err := ParseOneFoot(b)
			if err != nil {
				continue
			}
			progress(p)
			if p.Done {
				return nil
			}
		case <-ctx.Done():
			s.stopOneFoot()
			return ctx.Err()
		}
	}
}

func (s *Session) stopOneFoot() {
	ctx, cancel := context.WithTimeout(context.Background(), s.ResponseTimeout)
	defer cancel()
	_ = s.g.Write(ctx, ServiceHuami, CharControl, CmdOneFootStop())
}

// History downloads offline records for uid like HMWeightSyncDataProfile and
// filters invalid ones. received counts the raw records before the filter;
// the app acknowledges a transfer by it (gdsp/weight/OooO0O0.java:140-142).
// ErrIncompleteHistory means fewer records than announced arrived; then the
// transfer must not be acknowledged.
func (s *Session) History(ctx context.Context, uid uint32) (records []Measurement, received int, err error) {
	ch, err := s.subscribe(ctx, ServiceBodyComposition, CharHistory)
	if err != nil {
		return nil, 0, err
	}
	count, err := s.historyCount(ctx, ch, uid)
	if err != nil || count == 0 {
		return nil, 0, s.endEarly(ctx, err)
	}
	raw, err := s.historyFetch(ctx, ch)
	if err != nil {
		return nil, 0, err
	}
	if err := s.historyStop(ctx); err != nil {
		return nil, 0, err
	}
	now := s.Now()
	for i := range raw {
		if raw[i].ValidHistory(now) {
			records = append(records, raw[i])
		}
	}
	if len(raw) != count {
		return records, len(raw), fmt.Errorf("%w: got %d of %d records", ErrIncompleteHistory, len(raw), count)
	}
	return records, len(raw), nil
}

// endEarly sends stop after a failed or empty count unless the link or the
// context is gone, and returns the first error.
func (s *Session) endEarly(ctx context.Context, err error) error {
	if errors.Is(err, ErrDisconnected) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if serr := s.historyStop(ctx); err == nil {
		return serr
	}
	return err
}

func (s *Session) historyStop(ctx context.Context) error {
	return s.g.Write(ctx, ServiceBodyComposition, CharHistory, CmdHistoryStop())
}

// historyCount sends `01 uid` and parses the `01 count` answer.
func (s *Session) historyCount(ctx context.Context, ch <-chan []byte, uid uint32) (int, error) {
	if err := s.g.Write(ctx, ServiceBodyComposition, CharHistory, CmdHistoryCount(uid)); err != nil {
		return 0, err
	}
	timer := time.NewTimer(s.ResponseTimeout)
	defer timer.Stop()
	select {
	case b, ok := <-ch:
		if !ok {
			return 0, ErrDisconnected
		}
		return ParseHistoryCount(b)
	case <-timer.C:
		return 0, fmt.Errorf("history count: %w", ErrNoResponse)
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// historyFetch sends `02` and collects records until the end marker or
// HistoryIdle without data.
func (s *Session) historyFetch(ctx context.Context, ch <-chan []byte) ([]Measurement, error) {
	if err := s.g.Write(ctx, ServiceBodyComposition, CharHistory, CmdHistoryFetch()); err != nil {
		_ = s.historyStop(ctx)
		return nil, err
	}
	var records []Measurement
	idle := time.NewTimer(s.HistoryIdle)
	defer idle.Stop()
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return nil, ErrDisconnected
			}
			if IsHistoryEnd(b) {
				return records, nil
			}
			records = append(records, ParseHistoryRecords(b)...)
			idle.Reset(s.HistoryIdle)
		case <-idle.C:
			return records, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// AckHistory confirms a complete transfer; the scale then deletes the
// records of uid (verified on XMTZC05HM).
func (s *Session) AckHistory(ctx context.Context, uid uint32) error {
	return s.g.Write(ctx, ServiceBodyComposition, CharHistory, CmdHistoryAck(uid))
}

// Live streams measurements notified on 0x2A9C while connected.
func (s *Session) Live(ctx context.Context) (<-chan Measurement, error) {
	ch, err := s.subscribe(ctx, ServiceBodyComposition, CharMeasurement)
	if err != nil {
		return nil, err
	}
	out := make(chan Measurement, 16)
	go func() {
		defer close(out)
		for {
			select {
			case b, ok := <-ch:
				if !ok {
					return
				}
				if m, err := ParseMeasurement(b); err == nil {
					select {
					case out <- m:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}
