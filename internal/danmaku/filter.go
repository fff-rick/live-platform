package danmaku

import "strings"

type trieNode struct {
	children map[rune]*trieNode
	terminal bool
}

type SensitiveFilter struct{ root *trieNode }

func NewSensitiveFilter(words []string) *SensitiveFilter {
	f := &SensitiveFilter{root: &trieNode{children: map[rune]*trieNode{}}}
	for _, raw := range words {
		word := []rune(strings.ToLower(strings.TrimSpace(raw)))
		if len(word) == 0 {
			continue
		}
		n := f.root
		for _, r := range word {
			if n.children[r] == nil {
				n.children[r] = &trieNode{children: map[rune]*trieNode{}}
			}
			n = n.children[r]
		}
		n.terminal = true
	}
	return f
}

func (f *SensitiveFilter) Contains(text string) bool {
	if f == nil || f.root == nil {
		return false
	}
	runes := []rune(strings.ToLower(text))
	for i := range runes {
		n := f.root
		for j := i; j < len(runes); j++ {
			n = n.children[runes[j]]
			if n == nil {
				break
			}
			if n.terminal {
				return true
			}
		}
	}
	return false
}
