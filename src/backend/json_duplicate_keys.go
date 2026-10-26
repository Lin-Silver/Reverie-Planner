package main

import (
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "io"
)

// rejectDuplicateJSONKeys checks every object, including those nested inside
// arrays. encoding/json.Unmarshal silently accepts duplicate object keys,
// making mutation payloads ambiguous.
func rejectDuplicateJSONKeys(body []byte) error {
    decoder := json.NewDecoder(bytes.NewReader(body))
    if err := scanJSONValueKeys(decoder); err != nil {
        return err
    }
    if _, err := decoder.Token(); err != io.EOF {
        if err == nil {
            return errors.New("unexpected trailing JSON value")
        }
        return err
    }
    return nil
}

func scanJSONValueKeys(decoder *json.Decoder) error {
    token, err := decoder.Token()
    if err != nil {
        return err
    }
    delimiter, ok := token.(json.Delim)
    if !ok {
        return nil
    }
    switch delimiter {
    case '{':
        keys := make(map[string]struct{})
        for decoder.More() {
            token, err := decoder.Token()
            if err != nil {
                return err
            }
            key, ok := token.(string)
            if !ok {
                return errors.New("invalid JSON object key")
            }
            if _, exists := keys[key]; exists {
                return fmt.Errorf("duplicate JSON key %q", key)
            }
            keys[key] = struct{}{}
            if err := scanJSONValueKeys(decoder); err != nil {
                return err
            }
        }
        end, err := decoder.Token()
        if err != nil {
            return err
        }
        if end != json.Delim('}') {
            return errors.New("invalid JSON object end")
        }
    case '[':
        for decoder.More() {
            if err := scanJSONValueKeys(decoder); err != nil {
                return err
            }
        }
        end, err := decoder.Token()
        if err != nil {
            return err
        }
        if end != json.Delim(']') {
            return errors.New("invalid JSON array end")
        }
    default:
        return errors.New("unexpected JSON delimiter")
    }
    return nil
}
