package db

import (
	"encoding/json"
	"strings"
	"testing"

	"image-web/backend/internal/model"
)

func TestApplyCanvasPatchMergesElementsAndConnections(t *testing.T) {
	name := "renamed"
	patched, err := applyCanvasPatch(json.RawMessage(`{
		"id":"canvas-1",
		"name":"old",
		"elements":[{"id":"node-1","text":"old"},{"id":"node-2","text":"keep"}],
		"connections":[{"id":"edge-1","from":"node-1","to":"node-2"},{"id":"edge-2","from":"node-2","to":"node-1"}]
	}`), model.CanvasItemPatch{
		ID:   "canvas-1",
		Name: &name,
		Elements: []json.RawMessage{
			json.RawMessage(`{"id":"node-1","text":"new"}`),
			json.RawMessage(`{"id":"node-3","text":"added"}`),
		},
		DeletedElementIDs: []string{"node-2"},
		Connections: []json.RawMessage{
			json.RawMessage(`{"id":"edge-1","from":"node-3","to":"node-1"}`),
		},
		DeletedConnectionIDs: []string{"edge-2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Elements []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"elements"`
		Connections []struct {
			ID   string `json:"id"`
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(patched, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "canvas-1" || got.Name != "renamed" {
		t.Fatalf("unexpected canvas metadata: %#v", got)
	}
	if len(got.Elements) != 2 || got.Elements[0].ID != "node-1" || got.Elements[0].Text != "new" || got.Elements[1].ID != "node-3" {
		t.Fatalf("unexpected elements: %#v", got.Elements)
	}
	if len(got.Connections) != 1 || got.Connections[0].ID != "edge-1" || got.Connections[0].From != "node-3" || got.Connections[0].To != "node-1" {
		t.Fatalf("unexpected connections: %#v", got.Connections)
	}
}

func TestDecodeCanvasArrayRequiresCanvasIDs(t *testing.T) {
	items, err := decodeCanvasArray([]byte(`[{"id":"canvas-1","name":"A"},{"id":"canvas-2","name":"B"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || jsonObjectString(items[1], "name") != "B" {
		t.Fatalf("unexpected decoded canvases: %#v", items)
	}

	if _, err := decodeCanvasArray([]byte(`[{"name":"missing"}]`)); err == nil {
		t.Fatal("expected missing canvas id to fail")
	}
}

func TestPackCanvasForStorageRoundTripsLargeCanvas(t *testing.T) {
	canvas := json.RawMessage(`{"id":"canvas-1","name":"A","elements":[{"id":"node-1","text":"` + strings.Repeat("hello ", 9000) + `"}],"connections":[]}`)

	stored, err := packCanvasForStorage(canvas)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) >= len(canvas) {
		t.Fatalf("stored canvas was not compressed: original=%d stored=%d", len(canvas), len(stored))
	}
	restored, err := unpackCanvasFromStorage(stored)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(canvas) {
		t.Fatal("restored canvas did not match original")
	}
}

func TestUnpackCanvasFromStorageLeavesPlainCanvasUntouched(t *testing.T) {
	canvas := json.RawMessage(`{"id":"canvas-1","name":"plain","elements":[],"connections":[]}`)
	restored, err := unpackCanvasFromStorage(canvas)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(canvas) {
		t.Fatal("plain canvas changed during unpack")
	}
}
