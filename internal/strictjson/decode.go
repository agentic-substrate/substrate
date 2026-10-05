package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Decode validates the lowercase ASCII keys of local recovery formats.
func Decode(data []byte, destination any) error {
	if !ValidText(data) {
		return errors.New("JSON must contain valid UTF-8 and Unicode escapes")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := value(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("JSON must contain exactly one value")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(destination)
}

func value(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting exceeds 32 levels")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || !recoveryKey(name) || seen[name] {
				return errors.New("JSON object contains a duplicate or invalid key")
			}
			seen[name] = true
			if err := value(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := value(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = d.Token()
	return err
}

func recoveryKey(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if c != '_' && !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
