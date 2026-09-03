package serve

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// yamlToAny decodes YAML into JSON-marshalable values, matching what the
// retired Python server (PyYAML safe_load + json.dumps(default=str)) sent to
// the frontend, with the divergences the contract accepts: YAML 1.2 scalar
// resolution (bare yes/no/on/off stay strings) and NaN/Inf sanitized to
// their raw text instead of Python's invalid-JSON tokens.
func yamlToAny(data []byte) (any, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	if node.Kind == 0 {
		return nil, nil // empty document
	}
	return nodeToAny(&node)
}

var dateOnlyRE = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})$`)

func nodeToAny(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return nodeToAny(n.Content[0])
	case yaml.AliasNode:
		return nodeToAny(n.Alias)
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeToAny(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			v, err := nodeToAny(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[n.Content[i].Value] = v
		}
		return out, nil
	case yaml.ScalarNode:
		return scalarToAny(n)
	default:
		return nil, fmt.Errorf("unsupported yaml node kind %d", n.Kind)
	}
}

func scalarToAny(n *yaml.Node) (any, error) {
	switch n.Tag {
	case "!!null":
		return nil, nil
	case "!!bool":
		if b, err := strconv.ParseBool(strings.ToLower(n.Value)); err == nil {
			return b, nil
		}
		return n.Value, nil
	case "!!int":
		if i, err := strconv.ParseInt(n.Value, 0, 64); err == nil {
			return i, nil
		}
		return n.Value, nil
	case "!!float":
		lower := strings.ToLower(n.Value)
		if strings.Contains(lower, "inf") || strings.Contains(lower, "nan") {
			return n.Value, nil // JSON has no NaN/Inf; the raw text degrades gracefully
		}
		if f, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return f, nil
		}
		return n.Value, nil
	case "!!timestamp":
		return timestampString(n.Value), nil
	default:
		return n.Value, nil
	}
}

// timestampString renders a YAML timestamp the way PyYAML + str() did:
// dates zero-padded ("2026-09-01"), datetimes space-separated
// ("2026-09-01 19:19:00", fraction and "+00:00"-style offset when present).
func timestampString(raw string) string {
	if m := dateOnlyRE.FindStringSubmatch(raw); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		return fmt.Sprintf("%04d-%02d-%02d", y, mo, d)
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	} {
		t, err := time.Parse(layout, raw)
		if err != nil {
			continue
		}
		out := t.Format("2006-01-02 15:04:05")
		if ns := t.Nanosecond(); ns != 0 {
			out += strings.TrimRight(fmt.Sprintf(".%09d", ns), "0")
		}
		if strings.ContainsAny(raw, "Zz+") || strings.Count(raw, "-") > 2 {
			out += t.Format("-07:00")
		}
		return out
	}
	return raw
}

// jsonToAny decodes a .json task file, using json.Number so numbers
// round-trip exactly as written (Python's json.loads/dumps behavior).
func jsonToAny(data []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}
