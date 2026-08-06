package supplier

import (
	"strings"
	"testing"
)

// Not Agnes's table: proves the helper carries no provider-specific data.
var demoSizeTable = SizeTable{
	"1:1":  {"540p": {540, 540}, "1080p": {1080, 1080}},
	"4:5":  {"540p": {432, 540}},
}

func TestSizeTable_ResolveSize(t *testing.T) {
	w, h, err := demoSizeTable.ResolveSize("1:1", "1080p")
	if err != nil {
		t.Fatalf("ResolveSize() error = %v", err)
	}
	if w != 1080 || h != 1080 {
		t.Errorf("ResolveSize(1:1,1080p) = %dx%d, want 1080x1080", w, h)
	}
}

func TestSizeTable_RejectsUnknownAspectRatio(t *testing.T) {
	_, _, err := demoSizeTable.ResolveSize("9:16", "540p")
	if err == nil {
		t.Fatal("ResolveSize() error = nil, want unknown-aspect-ratio error")
	}
	if !strings.Contains(err.Error(), "unsupported aspect_ratio") {
		t.Errorf("error = %v, want it to mention unsupported aspect_ratio", err)
	}
	if !strings.Contains(err.Error(), "1:1") || !strings.Contains(err.Error(), "4:5") {
		t.Errorf("error = %v, want it to list supported aspect ratios", err)
	}
}

func TestSizeTable_RejectsUnknownResolution(t *testing.T) {
	_, _, err := demoSizeTable.ResolveSize("4:5", "1080p")
	if err == nil {
		t.Fatal("ResolveSize() error = nil, want unknown-resolution error")
	}
	if !strings.Contains(err.Error(), "unsupported resolution") {
		t.Errorf("error = %v, want it to mention unsupported resolution", err)
	}
}

func TestSizeTable_RejectsEmptyInputs(t *testing.T) {
	if _, _, err := demoSizeTable.ResolveSize("", "540p"); err == nil {
		t.Error("empty aspect_ratio should error")
	}
	if _, _, err := demoSizeTable.ResolveSize("1:1", ""); err == nil {
		t.Error("empty resolution should error")
	}
}
