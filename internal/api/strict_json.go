package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Check the exact DTO shape before decoding: unknown/case-folded/duplicate fields
// and null non-pointer values are rejected, including inside track arrays.
func strictJSON(w http.ResponseWriter, r *http.Request, max int64, target any) bool {
	content, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) || r.Header.Get("Content-Encoding") != "" {
		writeError(w, r, 415, "unsupported_media_type", "Use uncompressed UTF-8 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, max)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var size *http.MaxBytesError
		if errors.As(err, &size) {
			writeError(w, r, 413, "payload_too_large", "Request body exceeds the limit")
		} else {
			writeError(w, r, 400, "invalid_json", "Cannot read request body")
		}
		return false
	}
	if !utf8.Valid(data) {
		writeError(w, r, 400, "invalid_json", "JSON must be valid UTF-8")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err = checkShape(decoder, reflect.TypeOf(target).Elem()); err == nil {
		_, err = decoder.Token()
		if err == io.EOF {
			err = nil
		} else {
			err = errors.New("trailing JSON")
		}
	}
	if err != nil || json.Unmarshal(data, target) != nil {
		writeError(w, r, 400, "invalid_json", "Invalid JSON, duplicate or unsupported fields")
		return false
	}
	return true
}
func checkShape(d *json.Decoder, t reflect.Type) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	nullable := t.Kind() == reflect.Pointer
	if nullable {
		t = t.Elem()
	}
	bad := errors.New("invalid shape")
	if token == nil {
		if nullable {
			return nil
		}
		return bad
	}
	switch t.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return bad
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = field.Type
			}
		}
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			ft, known := fields[name]
			if !ok || !known || seen[name] {
				return bad
			}
			seen[name] = true
			if err = checkShape(d, ft); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return bad
		}
	case reflect.Slice:
		if token != json.Delim('[') {
			return bad
		}
		for d.More() {
			if err = checkShape(d, t.Elem()); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return bad
		}
	case reflect.String:
		if _, ok := token.(string); !ok {
			return bad
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return bad
		}
	case reflect.Int, reflect.Int64:
		if _, ok := token.(json.Number); !ok {
			return bad
		}
	default:
		return bad
	}
	return nil
}
