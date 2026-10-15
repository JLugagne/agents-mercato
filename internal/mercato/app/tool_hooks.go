package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

var errHookDocShape = errors.New("unexpected JSON shape")

func decodeJSONValue(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func canonicalJSON(data []byte) ([]byte, error) {
	v, err := decodeJSONValue(data)
	if err != nil {
		return nil, err
	}
	return marshalCanonical(v)
}

func readJSONDoc(read func(string) ([]byte, error), abs string) (map[string]any, error) {
	data, err := read(abs)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(data)) == 0) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := decodeJSONValue(data)
	if err != nil {
		return nil, err
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, errHookDocShape
	}
	return doc, nil
}

func writeJSONDoc(w txWriter, abs string, doc map[string]any) error {
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return w.WriteFile(abs, append(out, '\n'))
}

func eventContainer(doc map[string]any, keys []string, create bool) (map[string]any, error) {
	cur := doc
	for _, k := range keys {
		next, ok := cur[k]
		if !ok {
			if !create {
				return nil, nil
			}
			m := map[string]any{}
			cur[k] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return nil, errHookDocShape
		}
		cur = m
	}
	return cur, nil
}

func indexOfCanonical(list []any, canonical []byte) int {
	for i, e := range list {
		c, err := marshalCanonical(e)
		if err == nil && bytes.Equal(c, canonical) {
			return i
		}
	}
	return -1
}

func splitAnchor(anchor string) (file string, keys []string, event string, ok bool) {
	file, path, found := strings.Cut(anchor, "#")
	if !found || path == "" {
		return "", nil, "", false
	}
	parts := strings.Split(path, "/")
	return file, parts[:len(parts)-1], parts[len(parts)-1], true
}

func applyHookPatch(w txWriter, projectDir string, p domain.HookPatch) ([]byte, error) {
	canonical, err := canonicalJSON(p.Entry)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(projectDir, filepath.FromSlash(p.File))
	doc, err := readJSONDoc(w.ReadFile, abs)
	if err != nil {
		return nil, errors.Join(errors.New(p.File), err)
	}
	for k, v := range p.Header {
		if _, ok := doc[k]; !ok {
			doc[k] = v
		}
	}
	container, err := eventContainer(doc, p.Keys, true)
	if err != nil {
		return nil, errors.Join(errors.New(p.File), err)
	}
	var list []any
	if existing, ok := container[p.Event]; ok {
		if list, ok = existing.([]any); !ok {
			return nil, errors.Join(errors.New(p.File), errHookDocShape)
		}
	}
	if indexOfCanonical(list, canonical) >= 0 {
		return canonical, nil
	}
	entry, err := decodeJSONValue(canonical)
	if err != nil {
		return nil, err
	}
	container[p.Event] = append(list, entry)
	if err := writeJSONDoc(w, abs, doc); err != nil {
		return nil, err
	}
	return canonical, nil
}

func removeHookPatch(w txWriter, projectDir, anchor string, data []byte) error {
	file, keys, event, ok := splitAnchor(anchor)
	if !ok {
		return nil
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return err
	}
	abs := filepath.Join(projectDir, filepath.FromSlash(file))
	doc, err := readJSONDoc(w.ReadFile, abs)
	if err != nil {
		return errors.Join(errors.New(file), err)
	}
	chain := []map[string]any{doc}
	cur := doc
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		chain = append(chain, next)
		cur = next
	}
	list, ok := cur[event].([]any)
	if !ok {
		return nil
	}
	i := indexOfCanonical(list, canonical)
	if i < 0 {
		return nil
	}
	list = append(list[:i], list[i+1:]...)
	if len(list) == 0 {
		delete(cur, event)
	} else {
		cur[event] = list
	}
	for depth := len(keys); depth > 0; depth-- {
		if len(chain[depth]) > 0 {
			break
		}
		delete(chain[depth-1], keys[depth-1])
	}
	if len(doc) == 0 {
		if err := w.DeleteFile(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeJSONDoc(w, abs, doc)
}

func hookPatchPresent(read func(string) ([]byte, error), projectDir, anchor string, data []byte) (bool, error) {
	file, keys, event, ok := splitAnchor(anchor)
	if !ok {
		return false, nil
	}
	canonical, err := canonicalJSON(data)
	if err != nil {
		return false, err
	}
	doc, err := readJSONDoc(read, filepath.Join(projectDir, filepath.FromSlash(file)))
	if err != nil {
		return false, err
	}
	container, err := eventContainer(doc, keys, false)
	if err != nil || container == nil {
		return false, err
	}
	list, _ := container[event].([]any)
	return indexOfCanonical(list, canonical) >= 0, nil
}
