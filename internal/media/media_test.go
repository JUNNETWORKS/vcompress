package media

import "testing"

func TestParseFPS(t *testing.T) {
	got, err := ParseFPS("30000/1001")
	if err != nil {
		t.Fatal(err)
	}
	if got < 29.969 || got > 29.971 {
		t.Fatalf("ParseFPS() = %v", got)
	}
}

func TestDurationClose(t *testing.T) {
	if !DurationClose(100, 100.5) {
		t.Fatal("expected close durations")
	}
	if DurationClose(100, 105) {
		t.Fatal("expected durations to differ")
	}
}

func TestFPSClose(t *testing.T) {
	if !FPSClose("30000/1001", "29.97003") {
		t.Fatal("equivalent fps should be close")
	}
	if FPSClose("24/1", "30/1") {
		t.Fatal("24 and 30 fps should not be close")
	}
}
