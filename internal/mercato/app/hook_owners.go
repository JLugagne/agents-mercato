package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

const hookOwnersFile = "mct-hooks.json"

type hookOwner struct {
	Ref     domain.MctRef     `json:"ref,omitempty"`
	Event   string            `json:"event"`
	Matcher string            `json:"matcher,omitempty"`
	Hooks   []json.RawMessage `json:"hooks"`
}

type hookGroupRef struct {
	event string
	index int
}

type hookState struct {
	doc           settingsDoc
	hooks         map[string][]json.RawMessage
	owners        map[string]hookOwner
	ownersExisted bool
	settingsDirty bool
	ownersDirty   bool
}

func hookOwnersPath(settingsAbsPath string) string {
	return filepath.Join(filepath.Dir(settingsAbsPath), hookOwnersFile)
}

func readOptional(read func(string) ([]byte, error), path string) ([]byte, bool, error) {
	content, err := read(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

func loadHookState(read func(string) ([]byte, error), settingsAbsPath string) (*hookState, error) {
	settings, _, err := readOptional(read, settingsAbsPath)
	if err != nil {
		return nil, err
	}
	owners, existed, err := readOptional(read, hookOwnersPath(settingsAbsPath))
	if err != nil {
		return nil, err
	}
	return parseHookState(settings, owners, existed)
}

func parseHookState(settings, owners []byte, ownersExisted bool) (*hookState, error) {
	doc, err := readSettings(settings)
	if err != nil {
		return nil, err
	}
	hooks, err := doc.hooksByEvent()
	if err != nil {
		return nil, err
	}
	st := &hookState{doc: doc, hooks: hooks, owners: map[string]hookOwner{}, ownersExisted: ownersExisted}
	if len(bytes.TrimSpace(owners)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(owners))
		dec.UseNumber()
		if err := dec.Decode(&st.owners); err != nil {
			return nil, errors.Join(errors.New(hookOwnersFile), err)
		}
	}
	if err := st.migrateLegacy(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *hookState) migrateLegacy() error {
	for _, event := range sortedKeys(s.hooks) {
		list := s.hooks[event]
		out := make([]json.RawMessage, 0, len(list))
		groupAt := map[string]int{}
		var order []string
		handlers := map[string][]json.RawMessage{}
		matchers := map[string]string{}
		for _, raw := range list {
			var probe map[string]json.RawMessage
			if json.Unmarshal(raw, &probe) != nil {
				out = append(out, raw)
				continue
			}
			_, isGroup := probe["hooks"]
			idRaw, hasID := probe["mct_id"]
			var id string
			if hasID {
				_ = json.Unmarshal(idRaw, &id)
			}
			if isGroup || id == "" {
				out = append(out, raw)
				continue
			}
			if _, seen := groupAt[id]; !seen {
				groupAt[id] = len(out)
				out = append(out, nil)
				order = append(order, id)
				var m string
				if mr, ok := probe["matcher"]; ok {
					_ = json.Unmarshal(mr, &m)
				}
				matchers[id] = m
			}
			h, err := cleanHookHandler(raw)
			if err != nil {
				return err
			}
			handlers[id] = append(handlers[id], h)
		}
		if len(order) == 0 {
			continue
		}
		for _, id := range order {
			group, err := marshalHookGroup(matchers[id], handlers[id])
			if err != nil {
				return err
			}
			out[groupAt[id]] = group
			s.owners[id] = hookOwner{Ref: s.owners[id].Ref, Event: event, Matcher: matchers[id], Hooks: handlers[id]}
		}
		s.hooks[event] = out
		s.settingsDirty = true
		s.ownersDirty = true
	}
	return nil
}

func (s *hookState) resolve() map[string]hookGroupRef {
	resolved := map[string]hookGroupRef{}
	claimed := map[hookGroupRef]bool{}
	ids := sortedKeys(s.owners)
	for _, id := range ids {
		o := s.owners[id]
		for i, raw := range s.hooks[o.Event] {
			ref := hookGroupRef{event: o.Event, index: i}
			if claimed[ref] {
				continue
			}
			matcher, handlers, ok := parseHookGroup(raw)
			if ok && matcher == o.Matcher && sameHandlers(handlers, o.Hooks) {
				resolved[id] = ref
				claimed[ref] = true
				break
			}
		}
	}
	for _, id := range ids {
		if _, ok := resolved[id]; ok {
			continue
		}
		o := s.owners[id]
		var candidates []hookGroupRef
		for i, raw := range s.hooks[o.Event] {
			ref := hookGroupRef{event: o.Event, index: i}
			if claimed[ref] {
				continue
			}
			matcher, handlers, ok := parseHookGroup(raw)
			if ok && matcher == o.Matcher && sameHandlerTypes(handlers, o.Hooks) {
				candidates = append(candidates, ref)
			}
		}
		if len(candidates) == 1 {
			resolved[id] = candidates[0]
			claimed[candidates[0]] = true
		}
	}
	return resolved
}

func (s *hookState) conflicts(resolved map[string]hookGroupRef, event, matcher, mctID string) bool {
	for id := range resolved {
		o := s.owners[id]
		if id != mctID && o.Event == event && o.Matcher == matcher {
			return true
		}
	}
	return false
}

func (s *hookState) removeOwned(resolved map[string]hookGroupRef, mctID string) {
	if ref, ok := resolved[mctID]; ok {
		list := s.hooks[ref.event]
		s.hooks[ref.event] = append(list[:ref.index:ref.index], list[ref.index+1:]...)
		s.settingsDirty = true
	}
	if _, ok := s.owners[mctID]; ok {
		delete(s.owners, mctID)
		s.ownersDirty = true
	}
}

func (s *hookState) add(ref domain.MctRef, snippet domain.HookSnippet) ([]json.RawMessage, error) {
	handlers := make([]json.RawMessage, 0, len(snippet.Hooks))
	for _, raw := range snippet.Hooks {
		h, err := cleanHookHandler(raw)
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, h)
	}
	group, err := marshalHookGroup(snippet.Matcher, handlers)
	if err != nil {
		return nil, err
	}
	s.hooks[snippet.Event] = append(s.hooks[snippet.Event], group)
	s.owners[mctIDForRef(ref)] = hookOwner{Ref: ref, Event: snippet.Event, Matcher: snippet.Matcher, Hooks: handlers}
	s.settingsDirty = true
	s.ownersDirty = true
	return handlers, nil
}

func (s *hookState) save(w txWriter, settingsAbsPath string) error {
	if s.settingsDirty {
		if err := s.doc.setHooksByEvent(s.hooks); err != nil {
			return err
		}
		out, err := s.doc.marshal()
		if err != nil {
			return err
		}
		if err := w.WriteFile(settingsAbsPath, out); err != nil {
			return err
		}
	}
	if !s.ownersDirty {
		return nil
	}
	ownersPath := hookOwnersPath(settingsAbsPath)
	if len(s.owners) == 0 {
		if !s.ownersExisted {
			return nil
		}
		return w.DeleteFile(ownersPath)
	}
	out, err := json.MarshalIndent(s.owners, "", "  ")
	if err != nil {
		return err
	}
	return w.WriteFile(ownersPath, append(out, '\n'))
}

func parseHookGroup(raw json.RawMessage) (string, []json.RawMessage, bool) {
	var g struct {
		Matcher string            `json:"matcher"`
		Hooks   []json.RawMessage `json:"hooks"`
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal(raw, &probe) != nil {
		return "", nil, false
	}
	if _, ok := probe["hooks"]; !ok {
		return "", nil, false
	}
	if json.Unmarshal(raw, &g) != nil {
		return "", nil, false
	}
	return g.Matcher, g.Hooks, true
}

func marshalHookGroup(matcher string, handlers []json.RawMessage) (json.RawMessage, error) {
	arr, err := json.Marshal(handlers)
	if err != nil {
		return nil, err
	}
	obj := map[string]json.RawMessage{"hooks": arr}
	if matcher != "" {
		mb, err := json.Marshal(matcher)
		if err != nil {
			return nil, err
		}
		obj["matcher"] = mb
	}
	return marshalSortedObject(obj)
}

func cleanHookHandler(raw json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	delete(obj, "mct_id")
	delete(obj, "matcher")
	return marshalSortedObject(obj)
}

func sameHandlers(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, err := canonicalHookBody(a[i])
		if err != nil {
			return false
		}
		cb, err := canonicalHookBody(b[i])
		if err != nil || !bytes.Equal(ca, cb) {
			return false
		}
	}
	return true
}

func sameHandlerTypes(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if handlerType(a[i]) != handlerType(b[i]) {
			return false
		}
	}
	return true
}

func handlerType(raw json.RawMessage) string {
	var probe struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &probe)
	return probe.Type
}

func hookChecksum(matcher string, handlers []json.RawMessage) (string, error) {
	var concat []byte
	for _, raw := range handlers {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return "", err
		}
		delete(obj, "mct_id")
		if _, has := obj["matcher"]; !has && matcher != "" {
			obj["matcher"] = matcher
		}
		c, err := marshalCanonical(obj)
		if err != nil {
			return "", err
		}
		concat = append(concat, c...)
	}
	return xxhashHex(concat), nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
