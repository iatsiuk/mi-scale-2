package scale

import (
	"encoding/binary"
	"fmt"
	"time"
)

// GATT uuids, lowercase 128-bit.
const (
	huamiBase = "-0000-3512-2118-0009af100700"
	sigBase   = "-0000-1000-8000-00805f9b34fb"

	ServiceBodyComposition = "0000181b" + sigBase
	CharMeasurement        = "00002a9c" + sigBase
	CharCurrentTime        = "00002a2b" + sigBase
	CharHistory            = "00002a2f" + huamiBase

	ServiceHuami = "00001530" + huamiBase
	CharControl  = "00001542" + huamiBase
	CharStatus   = "00001543" + huamiBase

	ServiceDeviceInfo = "0000180a" + sigBase
	CharSystemID      = "00002a23" + sigBase
	CharSerial        = "00002a25" + sigBase
	CharHardwareRev   = "00002a27" + sigBase
	CharSoftwareRev   = "00002a28" + sigBase
	CharPnP           = "00002a50" + sigBase
)

// EncodeTime builds the 10-byte 0x2A2B payload. The app writes UTC and uses
// java Calendar.DAY_OF_WEEK (1 = sunday).
func EncodeTime(t time.Time) []byte {
	t = t.UTC()
	b := binary.LittleEndian.AppendUint16(nil, uint16(t.Year())) //nolint:gosec // years fit in 16 bits
	return append(b, u8(int(t.Month())), u8(t.Day()), u8(t.Hour()), u8(t.Minute()), u8(t.Second()),
		u8(int(t.Weekday())+1), 0, 0)
}

// u8 narrows calendar fields, which are always below 256.
func u8(v int) byte { return byte(v & 0xff) } //nolint:gosec // masked

// DecodeTime parses a 0x2A2B read.
func DecodeTime(b []byte) (time.Time, error) {
	if len(b) != 10 {
		return time.Time{}, fmt.Errorf("scale: time is %d bytes, want 10", len(b))
	}
	return time.Date(int(binary.LittleEndian.Uint16(b)), time.Month(b[2]), int(b[3]),
		int(b[4]), int(b[5]), int(b[6]), 0, time.UTC), nil
}

func control(cmd, arg byte) []byte { return []byte{0x06, cmd, 0x00, arg} }

// CmdSetUnit sets the scale display unit (fire-and-forget).
func CmdSetUnit(u Unit) []byte {
	var arg byte
	switch u {
	case UnitLb:
		arg = 1
	case UnitJin:
		arg = 2
	}
	return control(0x04, arg)
}

// CmdUserMode leaves mode 3; the app sends it on connect when needed.
func CmdUserMode() []byte { return control(0x0b, 0) }

// CmdEraseHistory deletes all offline records; answered with a Response.
func CmdEraseHistory() []byte { return control(0x12, 0) }

// CmdPartMeasure toggles partial measure mode; answered with a Response.
func CmdPartMeasure(enable bool) []byte {
	if enable {
		return control(0x10, 0)
	}
	return control(0x10, 1)
}

// CmdOneFootStart starts the one-foot balance test; answered with a Response
// followed by OneFoot progress notifications.
func CmdOneFootStart() []byte { return control(0x0f, 0) }

// CmdOneFootStop stops the one-foot balance test.
func CmdOneFootStop() []byte { return control(0x11, 0) }

type Status byte

const (
	StatusSuccess         Status = 1
	StatusInvalidState    Status = 2
	StatusUnknownCommand  Status = 3
	StatusOperationFailed Status = 4
)

func (s Status) String() string {
	switch s {
	case StatusSuccess:
		return "success"
	case StatusInvalidState:
		return "invalid state"
	case StatusUnknownCommand:
		return "unknown command"
	case StatusOperationFailed:
		return "operation failed"
	}
	return fmt.Sprintf("status(%d)", byte(s))
}

// Response is a 0x1542 notification `10 group cmd 00 status`.
type Response struct {
	Group   byte
	Command byte
	Status  Status
}

func (r Response) OK() bool { return r.Status == StatusSuccess }

func ParseResponse(b []byte) (Response, error) {
	if len(b) < 5 || b[0] != 0x10 {
		return Response{}, fmt.Errorf("scale: not a response: %x", b)
	}
	return Response{Group: b[1], Command: b[2], Status: Status(b[4])}, nil
}

// OneFoot is a 6-byte progress notification of the one-foot test.
type OneFoot struct {
	Done     bool
	Duration time.Duration
}

func ParseOneFoot(b []byte) (OneFoot, error) {
	if len(b) != 6 {
		return OneFoot{}, fmt.Errorf("scale: one-foot payload is %d bytes, want 6", len(b))
	}
	return OneFoot{
		Done:     b[3] == 2,
		Duration: time.Duration(binary.LittleEndian.Uint16(b[4:6])) * 100 * time.Millisecond,
	}, nil
}

// Mode is the 0x1542 read value.
type Mode uint16

func ParseMode(b []byte) (Mode, error) {
	if len(b) < 2 {
		return 0, errShort
	}
	return Mode(binary.LittleEndian.Uint16(b)), nil
}

func (m Mode) NeedsUserMode() bool { return m == 3 }

type Model int

const (
	ModelUnknown Model = iota
	ModelBodyCompositionScale
	ModelBodyCompositionScale2
)

func (m Model) String() string {
	switch m {
	case ModelBodyCompositionScale:
		return "Mi Body Composition Scale (XMTZC02HM)"
	case ModelBodyCompositionScale2:
		return "Mi Body Composition Scale 2 (XMTZC05HM)"
	}
	return "unknown"
}

// PnP is the Device Information PnP ID (0x2A50).
type PnP struct {
	VendorIDSource byte
	VendorID       uint16
	ProductID      uint16
	ProductVersion uint16
}

func ParsePnP(b []byte) (PnP, error) {
	if len(b) < 7 {
		return PnP{}, errShort
	}
	return PnP{
		VendorIDSource: b[0],
		VendorID:       binary.LittleEndian.Uint16(b[1:3]),
		ProductID:      binary.LittleEndian.Uint16(b[3:5]),
		ProductVersion: binary.LittleEndian.Uint16(b[5:7]),
	}, nil
}

// Model maps product ids like DeviceInfo: 6 -> source 101, 25 -> source 102.
// Both use the same protocol and body composition algorithm.
func (p PnP) Model() Model {
	switch p.ProductID {
	case 6:
		return ModelBodyCompositionScale
	case 25:
		return ModelBodyCompositionScale2
	}
	return ModelUnknown
}

func history(op byte, uid uint32) []byte {
	b := []byte{op, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(b[1:], uid)
	return b
}

// CmdHistoryCount asks how many records are stored for uid.
func CmdHistoryCount(uid uint32) []byte { return history(0x01, uid) }

// CmdHistoryFetch starts streaming records.
func CmdHistoryFetch() []byte { return []byte{0x02} }

// CmdHistoryStop ends the transfer.
func CmdHistoryStop() []byte { return []byte{0x03} }

// CmdHistoryAck confirms all records were received for uid.
func CmdHistoryAck(uid uint32) []byte { return history(0x04, uid) }

// ParseHistoryCount parses the `01 count16LE` answer.
func ParseHistoryCount(b []byte) (int, error) {
	if len(b) < 3 || b[0] != 0x01 {
		return 0, fmt.Errorf("scale: bad history count answer: %x", b)
	}
	return int(binary.LittleEndian.Uint16(b[1:3])), nil
}

// IsHistoryEnd reports the end-of-data marker (any payload of at most 1 byte).
func IsHistoryEnd(b []byte) bool { return len(b) <= 1 }

// ParseHistoryRecords splits a notification into 13-byte records; a shorter
// tail is dropped like WeightBfsRecordParser.
func ParseHistoryRecords(b []byte) []Measurement {
	var out []Measurement
	for len(b) >= measurementSize {
		m, _ := ParseMeasurement(b[:measurementSize])
		out = append(out, m)
		b = b[measurementSize:]
	}
	return out
}
