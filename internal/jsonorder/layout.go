package jsonorder

import (
	"bytes"
	"encoding/json"
)

type layoutInfo struct {
	inlinePaths []string
	commaSpace  bool
	colonSpace  bool
}

// detectLayout finds non-empty containers that were written on a single line.
// It only needs to understand JSON's structural punctuation; strings are
// skipped as opaque tokens.
func detectLayout(data []byte) layoutInfo {
	s := &layoutScanner{data: data}
	s.skipSpace()
	if s.i < len(s.data) {
		s.scanValue("")
	}
	return layoutInfo{
		inlinePaths: s.inlinePaths,
		commaSpace:  s.commaSpace,
		colonSpace:  s.colonSpace,
	}
}

type layoutScanner struct {
	data        []byte
	i           int
	inlinePaths []string
	commaSpace  bool
	colonSpace  bool
}

func (s *layoutScanner) scanValue(path string) {
	s.skipSpace()
	if s.i >= len(s.data) {
		return
	}
	switch s.data[s.i] {
	case '{':
		s.scanObject(path)
	case '[':
		s.scanArray(path)
	case '"':
		s.scanString()
	default:
		for s.i < len(s.data) {
			switch s.data[s.i] {
			case ',', '}', ']', ' ', '\t', '\r', '\n':
				return
			}
			s.i++
		}
	}
}

func (s *layoutScanner) scanObject(path string) {
	start := s.i
	s.i++ // {
	s.skipSpace()
	if s.i < len(s.data) && s.data[s.i] == '}' {
		s.i++
		return
	}
	for {
		s.skipSpace()
		if s.i >= len(s.data) {
			return
		}
		keyStart := s.i
		if s.data[s.i] == '"' {
			s.scanString()
		}
		key := ""
		if s.i > keyStart {
			raw := s.data[keyStart:s.i]
			var tmp Value
			if err := decodeStringToken(raw, &tmp); err == nil {
				key = tmp.Str
			}
		}
		s.skipSpace()
		if s.i < len(s.data) && s.data[s.i] == ':' {
			s.i++
			s.noteColonSpace()
		}
		s.scanValue(path + "/" + PointerEscape(key))
		s.skipSpace()
		if s.i >= len(s.data) {
			return
		}
		if s.data[s.i] == ',' {
			s.i++
			s.noteCommaSpace()
			continue
		}
		if s.data[s.i] == '}' {
			s.i++
			break
		}
		return
	}
	s.recordInline(path, start, s.i)
}

func (s *layoutScanner) scanArray(path string) {
	start := s.i
	s.i++ // [
	s.skipSpace()
	if s.i < len(s.data) && s.data[s.i] == ']' {
		s.i++
		return
	}
	index := 0
	for {
		s.scanValue(path + "/" + itoa(index))
		index++
		s.skipSpace()
		if s.i >= len(s.data) {
			return
		}
		if s.data[s.i] == ',' {
			s.i++
			s.noteCommaSpace()
			continue
		}
		if s.data[s.i] == ']' {
			s.i++
			break
		}
		return
	}
	s.recordInline(path, start, s.i)
}

func (s *layoutScanner) scanString() {
	if s.i >= len(s.data) || s.data[s.i] != '"' {
		return
	}
	s.i++
	for s.i < len(s.data) {
		switch s.data[s.i] {
		case '\\':
			s.i += 2
		case '"':
			s.i++
			return
		default:
			s.i++
		}
	}
}

func (s *layoutScanner) noteCommaSpace() {
	j := s.i
	for j < len(s.data) && (s.data[j] == ' ' || s.data[j] == '\t') {
		j++
	}
	if j > s.i {
		s.commaSpace = true
	}
}

func (s *layoutScanner) noteColonSpace() {
	j := s.i
	for j < len(s.data) && (s.data[j] == ' ' || s.data[j] == '\t') {
		j++
	}
	if j > s.i {
		s.colonSpace = true
	}
}

func (s *layoutScanner) recordInline(path string, start, end int) {
	if end <= start {
		return
	}
	if !bytes.ContainsRune(s.data[start:end], '\n') {
		s.inlinePaths = append(s.inlinePaths, path)
	}
}

func (s *layoutScanner) skipSpace() {
	for s.i < len(s.data) {
		switch s.data[s.i] {
		case ' ', '\t', '\r', '\n':
			s.i++
		default:
			return
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func decodeStringToken(raw []byte, out *Value) error {
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return err
	}
	out.Str = str
	return nil
}
