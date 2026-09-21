package jsonx_test

import (
	"encoding/json"
	"testing"

	"github.com/lk16/box/internal/jsonx"
)

func TestAnObjectKeepsTheOrderTheFileWroteItIn(t *testing.T) {
	object, ok := jsonx.AsObject(json.RawMessage(`{"b": 1, "a": 2, "c": 3}`))
	if !ok {
		t.Fatal("an object was not read as one")
	}
	if got := object.Keys(); got[0] != "b" || got[1] != "a" || got[2] != "c" {
		t.Fatalf("keys came back as %v", got)
	}
}

func TestGetReadsAValueAndSaysWhenThereIsNone(t *testing.T) {
	object, _ := jsonx.AsObject(json.RawMessage(`{"a": "one"}`))
	if value, ok := object.Get("a"); !ok || string(value) != `"one"` {
		t.Fatalf("a came back as %s, %v", value, ok)
	}
	if _, ok := object.Get("b"); ok {
		t.Fatal("a key the object lacks was found")
	}
}

func TestSetKeepsAnExistingKeyWhereItSitsAndAppendsANewOne(t *testing.T) {
	object := jsonx.Object{{Key: "a", Value: json.RawMessage(`1`)}}
	object = object.Set("a", json.RawMessage(`2`)).Set("b", json.RawMessage(`3`))
	if got := object.Keys(); got[0] != "a" || got[1] != "b" {
		t.Fatalf("keys came back as %v", got)
	}
	if value, _ := object.Get("a"); string(value) != "2" {
		t.Fatalf("a came back as %s", value)
	}
}

func TestAListIsNotAnObject(t *testing.T) {
	if _, ok := jsonx.AsObject(json.RawMessage(`["a"]`)); ok {
		t.Fatal("a list was read as an object")
	}
}

func TestParseRejectsTextThatIsNotJSON(t *testing.T) {
	if _, err := jsonx.Parse([]byte("{")); err == nil {
		t.Fatal("broken JSON was accepted")
	}
}

func TestTypeNameSpellsTheTypeTheWayTheFileDoes(t *testing.T) {
	for raw, want := range map[string]string{
		`null`: "null", `true`: "a boolean", `12`: "a number",
		`"x"`: "text", `[1]`: "a list", `{"a":1}`: "an object",
	} {
		if got := jsonx.TypeName(json.RawMessage(raw)); got != want {
			t.Errorf("%s was named %q, want %q", raw, got, want)
		}
	}
}

func TestANumberKeepsTheSpellingTheFileGaveIt(t *testing.T) {
	if number, ok := jsonx.AsNumber(json.RawMessage(`4`)); !ok || number != "4" {
		t.Fatalf("4 came back as %q, %v", number, ok)
	}
}

func TestWriteRendersAnObjectTheWayAHandEditedFileSpellsIt(t *testing.T) {
	object := jsonx.Object{
		{Key: "name", Value: jsonx.Text("")},
		{Key: "required_mounts", Value: jsonx.Text(map[string]string{"go": "the Go toolchain"})},
	}
	want := "{\n  \"name\": \"\",\n  \"required_mounts\": {\n    \"go\": \"the Go toolchain\"\n  }\n}\n"
	if got := string(jsonx.Write(object)); got != want {
		t.Fatalf("written as %q, want %q", got, want)
	}
}

func TestAnEmptyObjectIsWrittenAsOne(t *testing.T) {
	if got := string(jsonx.Write(jsonx.Object{})); got != "{}\n" {
		t.Fatalf("written as %q", got)
	}
}

func TestANumberIsSpelledTheWayAJSONReaderSpellsIt(t *testing.T) {
	for raw, want := range map[string]string{
		"4": "4", "-1": "-1", "-0": "0", "4.5": "4.5", "4.0": "4.0",
		"1e3": "1000.0", "1e20": "1e+20", "0.1": "0.1",
		"123456789012345678901234567890": "123456789012345678901234567890",
	} {
		got, ok := jsonx.AsNumber(json.RawMessage(raw))
		if !ok || got != want {
			t.Errorf("%s spelled %q, want %q", raw, got, want)
		}
	}
}

func TestARepeatedKeyKeepsTheLastValueAtTheFirstPlace(t *testing.T) {
	object, ok := jsonx.AsObject(json.RawMessage(`{"a": 1, "b": 2, "a": 3}`))
	if !ok {
		t.Fatal("an object was not read as one")
	}
	if got := object.Keys(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("keys came back as %v", got)
	}
	if value, _ := object.Get("a"); string(value) != "3" {
		t.Fatalf("a came back as %s", value)
	}
}
