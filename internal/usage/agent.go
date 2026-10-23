package usage

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Limits on what an agent can report. The agent sends at most 24 groups and
// 24 processes, so more isn't from Roost's agent.
const (
	agentMaxItems = 60
	maxKey        = 160
	maxGroupName  = 100
	maxTopName    = 60
	maxProcName   = 80
	maxUser       = 32
	// maxBytes is a petabyte, more memory than any one machine has.
	maxBytes = 1 << 50
)

// agentUsage is what the agent sends as a heartbeat's "usage", in arrays to
// keep it to a few kilobytes:
//
//	{"groups": [[key, cpu, mem, procs, [top names]], ...],
//	 "procs": [[pid, name, user, cpu, mem, group key, command], ...],
//	 "total": [cpu, mem, procs]}
//
// Each value is checked, since anyone with an agent's token can send it.
type agentUsage struct {
	Groups [][]any `json:"groups"`
	Procs  [][]any `json:"procs"`
	Total  []any   `json:"total"`
}

// ParseAgent reads the "usage" an agent sent at a moment, from a machine
// with the given number of cores. It returns false if there's nothing it can
// use.
func ParseAgent(raw json.RawMessage, at time.Time, cores int) (Sample, bool) {
	var u agentUsage
	if len(raw) == 0 || json.Unmarshal(raw, &u) != nil {
		return Sample{}, false
	}
	s := Sample{At: at, Source: SourceAgent, Cores: cores}
	seen := map[string]bool{}
	for _, g := range u.Groups[:min(agentMaxItems, len(u.Groups))] {
		key, ok := groupKey(item(g, 0))
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		kind, name := SplitKey(key)
		var names []string
		if top, ok := item(g, 4).([]any); ok {
			for _, t := range top[:min(3, len(top))] {
				if n := clean(t, maxTopName); n != "" {
					names = append(names, n)
				}
			}
		}
		s.Groups = append(s.Groups, Group{
			Key: key, Kind: kind, Name: name,
			CPU:   round2(percent(item(g, 1))),
			Mem:   byteCount(item(g, 2)),
			Procs: count(item(g, 3), 1e6),
			Top:   names,
		})
	}
	for _, p := range u.Procs[:min(agentMaxItems, len(u.Procs))] {
		name := clean(item(p, 1), maxProcName)
		if name == "" {
			continue
		}
		group, ok := groupKey(item(p, 5))
		if !ok {
			group = Key(KindOther, name)
		}
		s.Procs = append(s.Procs, Proc{
			PID:   count(item(p, 0), math.MaxInt32),
			Name:  name,
			User:  clean(item(p, 2), maxUser),
			CPU:   round2(percent(item(p, 3))),
			Mem:   byteCount(item(p, 4)),
			Group: group,
			Cmd:   clean(item(p, 6), maxCmd),
		})
	}
	if len(s.Groups) == 0 && len(s.Procs) == 0 {
		return Sample{}, false
	}
	slices.SortStableFunc(s.Groups, byUse(func(g Group) (float64, uint64) { return g.CPU, g.Mem }))
	slices.SortStableFunc(s.Procs, byUse(func(p Proc) (float64, uint64) { return p.CPU, p.Mem }))
	if len(u.Total) >= 3 {
		s.Total = &Totals{CPU: round2(percent(u.Total[0])), Mem: byteCount(u.Total[1]), Procs: count(u.Total[2], 1e7)}
	}
	return s, true
}

// item returns the value at index i of an array the agent sent, or nil.
func item(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

// groupKey returns the group key in v, as Key would make it, if v is one.
func groupKey(v any) (string, bool) {
	raw := clean(v, maxKey)
	if raw == "" {
		return "", false
	}
	kind, name := SplitKey(raw)
	name = cut(name, maxGroupName)
	if name == "" {
		return "", false
	}
	return Key(kind, name), true
}

// clean returns v if it's a string, on one line, trimmed and cut to at most n
// bytes, or else "".
func clean(v any, n int) string {
	s, _ := v.(string)
	s = strings.Join(strings.FieldsFunc(s, unicode.IsControl), " ")
	return cut(strings.TrimSpace(s), n)
}

// number returns v if it's a number, or else 0.
func number(v any) float64 {
	f, _ := v.(float64)
	return f
}

func percent(v any) float64 { return min(100, max(0, number(v))) }

func byteCount(v any) uint64 { return uint64(min(maxBytes, max(0, math.Round(number(v))))) }

func count(v any, most float64) int { return int(min(most, max(0, math.Round(number(v))))) }
