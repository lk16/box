// Package jsonx reads JSON objects while keeping the order their keys were written in.
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// RawValue is one JSON value as the file spells it.
type RawValue = json.RawMessage

// Pair is one member of a JSON object, at the position the file gave it.
type Pair struct {
	Key   string
	Value json.RawMessage
}

// Object is a JSON object whose keys keep the order the file wrote them in.
type Object []Pair

// Get returns the value stored under a key, and whether the object holds one.
func (o Object) Get(key string) (json.RawMessage, bool) {
	for _, pair := range o {
		if pair.Key == key {
			return pair.Value, true
		}
	}
	return nil, false
}

// Keys lists the object's keys in the order the file wrote them.
func (o Object) Keys() []string {
	keys := make([]string, 0, len(o))
	for _, pair := range o {
		keys = append(keys, pair.Key)
	}
	return keys
}

// Set stores a value, keeping an existing key where it already sits and appending a new one.
func (o Object) Set(key string, value json.RawMessage) Object {
	for index, pair := range o {
		if pair.Key == key {
			o[index].Value = value
			return o
		}
	}
	return append(o, Pair{Key: key, Value: value})
}

// Parse checks that bytes are one JSON document and hands back its text.
func Parse(data []byte) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// AsObject reads a value as an object, keeping its key order, and says whether it was one.
func AsObject(raw json.RawMessage) (Object, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if open, err := decoder.Token(); err != nil || open != json.Delim('{') {
		return nil, false
	}
	object := Object{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		object = append(object, Pair{Key: fmt.Sprint(key), Value: value})
	}
	return object, true
}

// isNull says whether a value is JSON's null, which unmarshals into anything without complaint.
func isNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// AsArray reads a value as a list of raw values, and says whether it was one.
func AsArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if isNull(raw) {
		return nil, false
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	return values, true
}

// AsString reads a value as text, and says whether it was text.
func AsString(raw json.RawMessage) (string, bool) {
	if isNull(raw) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return "", false
	}
	return text, true
}

// AsNumber spells a number the way a JSON reader does, and says whether it was one.
func AsNumber(raw json.RawMessage) (string, bool) {
	if isNull(raw) {
		return "", false
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return "", false
	}
	text := number.String()
	// A whole number spells itself, however long, since no float could hold all of its digits.
	if !strings.ContainsAny(text, ".eE") {
		return wholeNumber(text), true
	}
	return fraction(number)
}

// wholeNumber drops what a reader of the digits would not write back, such as the sign on -0.
func wholeNumber(text string) string {
	whole, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return text
	}
	return strconv.FormatInt(whole, 10)
}

// fraction spells a number written with a point or an exponent, which always reads back with one.
func fraction(number json.Number) (string, bool) {
	value, err := number.Float64()
	if err != nil {
		return number.String(), true
	}
	text := strconv.FormatFloat(value, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return text, true
}

// TypeName names a value's type the way the file that holds it spells it.
func TypeName(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "nothing"
	}
	switch text[0] {
	case '{':
		return "an object"
	case '[':
		return "a list"
	case '"':
		return "text"
	case 't', 'f':
		return "a boolean"
	case 'n':
		return "null"
	}
	return "a number"
}

// Text renders a value as JSON.
func Text(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	// Every value box writes is a string, a list or an object, none of which can fail to encode.
	if err != nil {
		panic(err)
	}
	return encoded
}

// Write renders an object the way a hand-edited file spells it: two spaces in, newline at the end.
func Write(object Object) []byte {
	if len(object) == 0 {
		return []byte("{}\n")
	}
	var out bytes.Buffer
	out.WriteString("{\n")
	for index, pair := range object {
		key, _ := json.Marshal(pair.Key)
		out.WriteString("  " + string(key) + ": " + indent(pair.Value))
		if index < len(object)-1 {
			out.WriteString(",")
		}
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return out.Bytes()
}

// indent re-renders one value at the depth a top-level member of an object sits at.
func indent(value json.RawMessage) string {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, value, "  ", "  "); err != nil {
		return string(value)
	}
	return pretty.String()
}
