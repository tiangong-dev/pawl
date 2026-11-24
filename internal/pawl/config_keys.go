package pawl

// Unknown-key warnings. A key the config schema does not define is dropped by
// yaml.Unmarshal, so a typo like `gat:` for `gate:` silently reverts a
// dimension to its defaults; these helpers surface that as an advisory warning
// (never an error, never an exit-code change). See spec/commands/config.md.

import (
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// warnUnknownConfigKeys reports on warn every key in the document that the
// config schema does not define. The keys were already dropped by the parse
// that ran before this call, so this is the only place they are still visible;
// it never changes the caller's control flow.
func warnUnknownConfigKeys(data []byte, path string, warn io.Writer) {
	for _, msg := range unknownConfigKeys(data) {
		fmt.Fprintln(warn, configWarningLine(path, msg))
	}
}

// unknownConfigKey is one mapping key the config schema does not define: the
// path that locates it in the document, and — when a defined key is a plausible
// typo — the key its author probably meant. message is the advisory form the
// warning prints; the exported-in-JSON fields are what a command that must act
// on the finding (validate) reports, so a caller never has to re-scan the
// document or scrape the suggestion back out of the sentence.
type unknownConfigKey struct {
	Path       string `json:"path"`
	Suggestion string `json:"suggestion"`
}

func (k unknownConfigKey) message() string {
	msg := fmt.Sprintf("unknown key %q", k.Path)
	if k.Suggestion != "" {
		msg += fmt.Sprintf(" — did you mean %q?", k.Suggestion)
	}
	return msg
}

// unknownConfigKeys reports the advisory message for every stray key, in
// document order.
func unknownConfigKeys(data []byte) []string {
	keys := findUnknownConfigKeys(data)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.message())
	}
	return out
}

// findUnknownConfigKeys scans the config document for mapping keys the schema
// does not define — at the top level, inside each analyzers[] entry, and inside
// each dimensions[] entry. Keys prefixed `x-` are reserved for user extensions
// (people park YAML anchors there) and are never reported. The scan reads the
// raw document, so it sees the keys yaml.Unmarshal silently dropped, and
// reports every one rather than stopping at the first. Merge keys and aliases
// are skipped: `<<` is a YAML keyword, not a user key, and the keys a merge
// splices in are not visible here — a documented limitation.
func findUnknownConfigKeys(data []byte) []unknownConfigKey {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	// A non-mapping top level (an alias, a sequence fragment) has no keys of
	// its own to check, and must not crash the scan.
	if root.Kind != yaml.MappingNode {
		return nil
	}
	topKeys := yamlKeySet(configFile{})
	analyzerKeys := yamlKeySet(analyzerConfig{})
	dimensionKeys := yamlKeySet(dimensionConfig{})

	var out []unknownConfigKey
	for i := 0; i+1 < len(root.Content); i += 2 {
		keyNode := root.Content[i]
		if isMergeKey(keyNode) {
			continue
		}
		key := keyNode.Value
		if isExtensionKey(key) {
			continue
		}
		if !topKeys[key] {
			out = append(out, unknownConfigKey{Path: key, Suggestion: closestKey(key, topKeys)})
		}
		switch key {
		case "analyzers":
			out = append(out, itemUnknownKeys(root.Content[i+1], "analyzers", analyzerKeys)...)
		case "dimensions":
			out = append(out, itemUnknownKeys(root.Content[i+1], "dimensions", dimensionKeys)...)
		}
	}
	return out
}

// itemUnknownKeys checks each mapping in a sequence-valued entry, labeling a
// stray key `<prefix>[<index>].<key>` — the path a reader needs to locate the
// typo in a file with several dimensions. A non-sequence value (an alias to a
// sequence, say) is skipped, as is an item that is an alias rather than a
// mapping: its keys live in the anchored mapping checked at its own position.
func itemUnknownKeys(seq *yaml.Node, prefix string, allowed map[string]bool) []unknownConfigKey {
	if seq.Kind != yaml.SequenceNode {
		return nil
	}
	var out []unknownConfigKey
	for i, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(item.Content); j += 2 {
			keyNode := item.Content[j]
			if isMergeKey(keyNode) {
				continue
			}
			key := keyNode.Value
			if isExtensionKey(key) || allowed[key] {
				continue
			}
			path := fmt.Sprintf("%s[%d].%s", prefix, i, key)
			out = append(out, unknownConfigKey{Path: path, Suggestion: closestKey(key, allowed)})
		}
	}
	return out
}

// isMergeKey reports whether a key node is the YAML merge key. Only a plain
// `<<` carries the `!!merge` tag; a quoted `"<<"` is an ordinary string key and
// keeps its `!!str` tag, so it is still checked like any other key.
func isMergeKey(key *yaml.Node) bool {
	return key.Tag == "!!merge"
}

// isExtensionKey reports whether a key is reserved for user extensions; the
// `x-` prefix is the conventional escape hatch for anchors and tooling.
func isExtensionKey(key string) bool {
	return strings.HasPrefix(key, "x-")
}

// yamlKeySet derives the accepted key names from a config struct's field tags,
// so the unknown-key warning cannot drift from what the parser accepts: declare
// a new field and it is accepted, delete the tag and it is not.
func yamlKeySet(sample any) map[string]bool {
	t := reflect.TypeOf(sample)
	keys := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		keys[name] = true
	}
	return keys
}

// closestKey returns the accepted key nearest to name by edit distance, or ""
// when nothing is close enough to be a plausible typo. Ties break
// lexicographically, so the same config always yields the same advice.
func closestKey(name string, allowed map[string]bool) string {
	const maxDistance = 2
	best, bestDistance := "", maxDistance+1
	for _, key := range sortedKeyNames(allowed) {
		if d := editDistance(name, key); d < bestDistance {
			best, bestDistance = key, d
		}
	}
	if bestDistance > maxDistance {
		return ""
	}
	return best
}

func sortedKeyNames(keys map[string]bool) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// editDistance is the Levenshtein distance between a and b, in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

// configWarningLine prefixes one advisory message with the config path and, on
// CI, the GitHub workflow-warning marker.
func configWarningLine(path, msg string) string {
	line := fmt.Sprintf("%s: %s", path, msg)
	if onCI() {
		return "::warning::" + line
	}
	return "warning: " + line
}
