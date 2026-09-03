package importer

import (
	"strings"
	"testing"
)

func TestDecodeSingleObject(t *testing.T) {
	tickets, err := Decode(strings.NewReader(`{"id":"x","title":"t"}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("want 1 ticket, got %d", len(tickets))
	}
	if tickets[0].ID != "x" {
		t.Errorf("ID = %q, want x", tickets[0].ID)
	}
}

func TestDecodeArray(t *testing.T) {
	tickets, err := Decode(strings.NewReader(`[{"id":"a","title":"First"},{"id":"b","title":"Second"}]`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(tickets) != 2 {
		t.Fatalf("want 2 tickets, got %d", len(tickets))
	}
	if tickets[0].ID != "a" || tickets[1].ID != "b" {
		t.Errorf("IDs = %q, %q", tickets[0].ID, tickets[1].ID)
	}
}

func TestDecodeEmpty(t *testing.T) {
	tickets, err := Decode(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Decode empty: %v", err)
	}
	if len(tickets) != 0 {
		t.Errorf("want 0 tickets, got %d", len(tickets))
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	_, err := Decode(strings.NewReader("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
