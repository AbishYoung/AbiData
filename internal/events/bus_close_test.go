package events

import "testing"

func TestBusCloseEndsSubscriptions(t *testing.T) {
	h := NewHub(10)
	s := h.Bus("w").Subscribe()
	h.Close()
	if _, ok := <-s.C; ok {
		t.Fatal("subscription still open after Close")
	}
	late := h.Bus("other").Subscribe()
	if _, ok := <-late.C; ok {
		t.Fatal("a bus created after Close accepted a subscriber")
	}
	s.Close() // safe after Close
	h.Publish("w", "x", nil)
}
