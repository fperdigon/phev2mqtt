package protocol

import (
	"encoding/hex"
	"testing"

	"gopkg.in/d4l3k/messagediff.v1"
)

func TestDecodeEncodeBytes(t *testing.T) {
	tests := []struct {
		in   string
		sk   *SecurityKey
		want *PhevMessage
	}{
		{
			in: "f60400060303",
			sk: &SecurityKey{keyMap: []byte{0x00, 0x00}},
			want: &PhevMessage{
				Type:          0xf6,
				Length:        0x6,
				Register:      0x6,
				Data:          []byte{0x3},
				Checksum:      0x3,
				Original:      []byte{0xf6, 0x4, 0x0, 0x6, 0x3, 0x3},
				OriginalXored: []byte{0xf6, 0x4, 0x0, 0x6, 0x3, 0x3},
			},
		}, {
			in: "502f3fff0f0f0a0d0f0d0d0f0f0f2f3e3f04",
			sk: &SecurityKey{keyMap: []byte{0x3f, 0x3f, 0x3f}},
			want: &PhevMessage{
				Type:          0x6f,
				Length:        0x12,
				Register:      0xc0,
				Data:          []byte{0x30, 0x30, 0x35, 0x32, 0x30, 0x32, 0x32, 0x30, 0x30, 0x30, 0x10, 0x1, 0x0},
				Checksum:      0x3b,
				Xor:           0x3f,
				Original:      []byte{0x6f, 0x10, 0x0, 0xc0, 0x30, 0x30, 0x35, 0x32, 0x30, 0x32, 0x32, 0x30, 0x30, 0x30, 0x10, 0x1, 0x0, 0x3b},
				OriginalXored: []byte{0x50, 0x2f, 0x3f, 0xff, 0x0f, 0x0f, 0x0a, 0x0d, 0x0f, 0x0d, 0x0d, 0x0f, 0x0f, 0x0f, 0x2f, 0x3e, 0x3f, 0x04},
			},
		}, {
			in: "caa2a5a7a5a5a5a5dd",
			sk: &SecurityKey{keyMap: []byte{0xa5, 0xa5}},
			want: &PhevMessage{
				Type:          0x6f,
				Length:        0x9,
				Register:      0x2,
				Data:          []byte{0x0, 0x0, 0x0, 0x0},
				Checksum:      0x78,
				Xor:           0xa5,
				Original:      []byte{0x6f, 0x7, 0x0, 0x2, 0x0, 0x0, 0x0, 0x0, 0x78},
				OriginalXored: []byte{0xca, 0xa2, 0xa5, 0xa7, 0xa5, 0xa5, 0xa5, 0xa5, 0xdd},
			},
		}, {
			in: "3cf4f13360d4",
			sk: &SecurityKey{keyMap: []byte{0xf0, 0xa5}},
			want: &PhevMessage{
				Type:          0xcc,
				Length:        0x6,
				Register:      0xc3,
				Data:          []byte{0x90},
				Checksum:      0x24,
				Xor:           0xf0,
				Ack:           Ack,
				Original:      []byte{0xcc, 0x4, 0x1, 0xc3, 0x90, 0x24},
				OriginalXored: []byte{0x3c, 0xf4, 0xf1, 0x33, 0x60, 0xd4},
			},
		}, {
			in: "4bf4f1c190a1",
			sk: &SecurityKey{keyMap: []byte{0xf0, 0xa5}},
			want: &PhevMessage{
				Type:          0xbb,
				Length:        0x6,
				Register:      0x31,
				Data:          []byte{0x60},
				Checksum:      0x51,
				Xor:           0xf0,
				Ack:           Ack,
				Original:      []byte{0xbb, 0x4, 0x1, 0x31, 0x60, 0x51},
				OriginalXored: []byte{0x4b, 0xf4, 0xf1, 0xc1, 0x90, 0xa1},
			},
		}, {
			in: "9ff6f0f3f1e59301",
			sk: &SecurityKey{keyMap: []byte{0xf0, 0xa5}},
			want: &PhevMessage{
				Type:          0x6f,
				Length:        0x8,
				Register:      0x3,
				Data:          []byte{0x1, 0x15, 0x63},
				Checksum:      0xf1,
				Xor:           0xf0,
				Original:      []byte{0x6f, 0x6, 0x0, 0x3, 0x1, 0x15, 0x63, 0xf1},
				OriginalXored: []byte{0x9f, 0xf6, 0xf0, 0xf3, 0xf1, 0xe5, 0x93, 0x01},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			data, err := hex.DecodeString(test.in)
			if err != nil {
				t.Fatal(err)
			}
			p := &PhevMessage{}
			if err := p.DecodeFromBytes(data, test.sk); err != nil {
				t.Fatalf("DecodeFromBytes() unexpected error: %v", err)
			}
			p.Reg = nil // Skip reg test for now.
			if diff, eq := messagediff.PrettyDiff(test.want, p); !eq {
				t.Fatalf("DecodeFromBytes() diff=%s", diff)
			}

			outData := test.want.EncodeToBytes(test.sk)
			gotData := hex.EncodeToString(outData)
			if gotData != test.in {
				t.Fatalf("EncodeToBytes: Unexpected. got=%s want=%s", gotData, test.in)
			}
		})
	}
}

// TestNewFromBytesMultiFrame verifies that two back-to-back frames in a
// single TCP read are both decoded.
func TestNewFromBytesMultiFrame(t *testing.T) {
	// Two valid zero-XOR frames concatenated.
	// Frame 1: f6 04 00 06 03 03
	// Frame 2: f6 04 00 06 03 03
	raw, _ := hex.DecodeString("f60400060303f60400060303")
	key := &SecurityKey{keyMap: []byte{0x00, 0x00}}
	msgs := NewFromBytes(raw, key)
	if len(msgs) != 2 {
		t.Fatalf("NewFromBytes: got %d messages, want 2", len(msgs))
	}
}

// TestNewFromBytesKeyProtection verifies that a misaligned CmdInMy18StartReq
// frame (offset > 0) does not corrupt the live SecurityKey.
func TestNewFromBytesKeyProtection(t *testing.T) {
	// Build a synthetic buffer: 1 junk byte followed by a valid CmdInMy18StartReq
	// (0x5e) frame with a known key-update packet, followed by a normal frame.
	//
	// Start18 raw (zero XOR):  5e 0c 00 01 be cf e9 ad ad a5 15 8b 01 81
	// After key.Update() with this raw packet, keyMap[0] = 246.
	//
	// We prepend 0xff as a junk byte so the scanner must skip 1 byte (offset=1)
	// to find the Start18. The live key should NOT be updated.

	start18Hex := "5e0c0001becfe9adada5158b0181"
	pingHex := "f60400060303" // a simple zero-XOR frame after

	junk := []byte{0xff}
	start18, _ := hex.DecodeString(start18Hex)
	ping, _ := hex.DecodeString(pingHex)

	raw := append(junk, start18...)
	raw = append(raw, ping...)

	key := &SecurityKey{keyMap: make([]byte, 256)}
	for i := range key.keyMap {
		key.keyMap[i] = byte(i)
	}
	origKeyMap0 := key.keyMap[0] // should stay 0x00

	msgs := NewFromBytes(raw, key)
	// We get 2 messages: the misaligned Start18 and the ping.
	if len(msgs) < 1 {
		t.Fatalf("expected at least 1 message, got 0")
	}

	// The live key must NOT have been mutated by the Start18 at offset 1.
	if key.keyMap[0] != origKeyMap0 {
		t.Errorf("live key was mutated by misaligned Start18: keyMap[0] got=%d want=%d",
			key.keyMap[0], origKeyMap0)
	}
}

// TestNewFromBytesNoDoubleXOR verifies that the round-trip through
// NewFromBytes → EncodeToBytes is idempotent (no double-XOR corruption).
func TestNewFromBytesNoDoubleXOR(t *testing.T) {
	// XOR'd frame: xor=0xf0, decoded=f6 04 00 06 03 03
	rawHex := "06f4f0f6f3f3"
	raw, _ := hex.DecodeString(rawHex)
	key := &SecurityKey{keyMap: []byte{0xf0, 0xa5}}

	msgs := NewFromBytes(raw, key)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.Type != 0xf6 {
		t.Errorf("Type: got=0x%02x want=0xf6", m.Type)
	}
	if hex.EncodeToString(m.Original) != "f60400060303" {
		t.Errorf("Original: got=%s want=f60400060303", hex.EncodeToString(m.Original))
	}
	if hex.EncodeToString(m.OriginalXored) != rawHex {
		t.Errorf("OriginalXored: got=%s want=%s", hex.EncodeToString(m.OriginalXored), rawHex)
	}
}

func TestRegisterChargeStatusDecode(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		wantCharging  bool
		wantRemaining int
	}{
		{
			name:          "not charging, 0xff sentinel",
			data:          []byte{0x00, 0x00, 0xff},
			wantCharging:  false,
			wantRemaining: 0,
		},
		{
			name:          "charging, 45 minutes",
			data:          []byte{0x01, 0x2d, 0x00},
			wantCharging:  true,
			wantRemaining: 0x002d, // 45
		},
		{
			name:          "charging, multi-byte remaining",
			data:          []byte{0x01, 0x20, 0x01},
			wantCharging:  true,
			wantRemaining: 0x0120, // 288
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &PhevMessage{
				Register: ChargeStatusRegister,
				Data:     tt.data,
			}
			reg := &RegisterChargeStatus{}
			reg.Decode(msg)
			if reg.Charging != tt.wantCharging {
				t.Errorf("Charging: got=%v want=%v", reg.Charging, tt.wantCharging)
			}
			if reg.Remaining != tt.wantRemaining {
				t.Errorf("Remaining: got=%d want=%d", reg.Remaining, tt.wantRemaining)
			}
		})
	}
}

func TestRegisterDoorStatusDecode(t *testing.T) {
	// Data layout: [0]=locked [3]=driver [4]=frontPassenger [5]=rearRight
	//              [6]=rearLeft [7]=boot [8]=bonnet [9]=headlights
	data := []byte{0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01, 0x00, 0x01}
	msg := &PhevMessage{
		Register: DoorStatusRegister,
		Data:     data,
	}
	reg := &RegisterDoorStatus{}
	reg.Decode(msg)

	if !reg.Locked {
		t.Error("Locked: want true")
	}
	if !reg.Driver {
		t.Error("Driver: want true")
	}
	if reg.FrontPassenger {
		t.Error("FrontPassenger: want false")
	}
	if !reg.RearRight {
		t.Error("RearRight: want true")
	}
	if reg.RearLeft {
		t.Error("RearLeft: want false")
	}
	if !reg.Boot {
		t.Error("Boot: want true")
	}
	if reg.Bonnet {
		t.Error("Bonnet: want false")
	}
	if !reg.Headlights {
		t.Error("Headlights: want true")
	}
}
