package taskbench

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// Typed decoders need the protocol's structural checks, not a second decoded
// value tree. The standard scanner owns JSON grammar; this bounded walk checks
// decoded keys, depth and nulls without allocating primitive values.
func validateWindowJSONShape(raw []byte, nonnull bool) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("invalid UTF-8 JSON")
	}
	s := windowJSONShape{raw: raw}
	s.space()
	if !json.Valid(raw) || s.pos == len(raw) || raw[s.pos] != '{' {
		// Preserve the existing rejection and diagnostic for malformed grammar,
		// trailing values and non-object roots. This is not the hot valid path.
		_, err := windowJSONObject(raw)
		if err == nil {
			return fmt.Errorf("invalid JSON object")
		}
		return err
	}
	if err := s.value(0); err != nil {
		return err
	}
	s.space()
	if s.pos != len(raw) {
		return fmt.Errorf("trailing JSON")
	}
	if nonnull && s.null {
		return fmt.Errorf("null actual readiness field")
	}
	return nil
}

type windowJSONShape struct {
	raw  []byte
	pos  int
	null bool
}

func (s *windowJSONShape) space() {
	for s.pos < len(s.raw) {
		switch s.raw[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
		default:
			return
		}
	}
}

// Called only after json.Valid. Escapes skip their next byte, so an escaped
// quote is never mistaken for the closing quote. Unicode escapes need no value
// decoding except in object keys, which use the same standard JSON semantics.
func (s *windowJSONShape) quoted() (start int, escaped bool) {
	start = s.pos
	s.pos++
	for s.raw[s.pos] != '"' {
		if s.raw[s.pos] == '\\' {
			escaped = true
			s.pos++
		}
		s.pos++
	}
	s.pos++
	return start, escaped
}

func (s *windowJSONShape) value(depth int) error {
	if depth > 128 {
		return fmt.Errorf("JSON depth exceeded")
	}
	s.space()
	switch s.raw[s.pos] {
	case '{':
		s.pos++
		s.space()
		keys := make(map[string]struct{})
		for s.raw[s.pos] != '}' {
			start, escaped := s.quoted()
			var key string
			if escaped {
				if err := json.Unmarshal(s.raw[start:s.pos], &key); err != nil {
					return fmt.Errorf("invalid JSON key")
				}
			} else {
				key = string(s.raw[start+1 : s.pos-1])
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("duplicate JSON key")
			}
			keys[key] = struct{}{}
			s.space()
			s.pos++ // colon, already grammar-checked
			if err := s.value(depth + 1); err != nil {
				return err
			}
			s.space()
			if s.raw[s.pos] == ',' {
				s.pos++
				s.space()
			}
		}
		s.pos++
	case '[':
		s.pos++
		s.space()
		for s.raw[s.pos] != ']' {
			if err := s.value(depth + 1); err != nil {
				return err
			}
			s.space()
			if s.raw[s.pos] == ',' {
				s.pos++
				s.space()
			}
		}
		s.pos++
	case '"':
		s.quoted()
	default:
		s.null = s.null || s.raw[s.pos] == 'n'
		for s.pos < len(s.raw) {
			switch s.raw[s.pos] {
			case ',', '}', ']', ' ', '\t', '\r', '\n':
				return nil
			default:
				s.pos++
			}
		}
	}
	return nil
}
