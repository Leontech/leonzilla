package main

import (
	"encoding/json"
	"testing"
)

func TestBuildLightNotes(t *testing.T) {
	// an existing card without notes stays exactly as it was
	old := `{"script":"C:\\x\\build.py","args":"mb","moreArgs":["cp","all","all --boot","cpboot","all --boot"],"lastArgs":"all --boot"}`
	var b BuildLight
	if err := json.Unmarshal([]byte(old), &b); err != nil {
		t.Fatal(err)
	}
	b.normalize()
	out, _ := json.Marshal(b)
	if string(out) != `{"script":"C:\\x\\build.py","args":"mb","moreArgs":["cp","all","all --boot","cpboot"],"lastArgs":"all --boot"}` {
		t.Fatalf("changed: %s", out)
	}
	// notes move with their variants, a duplicate gives its note to the first one
	b = BuildLight{Script: "a.py", Args: "mb", MoreArgs: []string{"cp", "cp", "all"}, ArgNotes: []string{"main board", "", "control panel", "  "}, Note: "  FW  build "}
	b.normalize()
	if b.Note != "FW build" || len(b.MoreArgs) != 2 || len(b.ArgNotes) != 2 || b.ArgNotes[0] != "main board" || b.ArgNotes[1] != "control panel" {
		t.Fatalf("%+v", b)
	}
}
