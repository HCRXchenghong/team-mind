package live

import (
	"sort"
	"strings"
	"unicode"
)

type memoryCandidate struct {
	Category   string
	Content    string
	Importance int
	Embedding  []float32
}

var memoryStopRunes = map[rune]bool{
	'的': true, '了': true, '我': true, '你': true, '是': true, '在': true, '有': true,
	'和': true, '与': true, '就': true, '都': true, '也': true, '很': true, '吗': true,
	'呢': true, '吧': true, '啊': true, '这': true, '那': true, '个': true, '要': true,
	'会': true, '可': true, '以': true, '不': true,
}

func memoryQueryTokens(text string) map[string]struct{} {
	tokens := map[string]struct{}{}
	text = strings.ToLower(truncateRunes(text, 2000))
	segments := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, segment := range segments {
		runes := []rune(segment)
		if len(runes) == 0 {
			continue
		}
		allHan := true
		for _, r := range runes {
			if !unicode.Is(unicode.Han, r) {
				allHan = false
				break
			}
		}
		if allHan {
			for i, r := range runes {
				if !memoryStopRunes[r] {
					tokens[string(r)] = struct{}{}
				}
				if i+1 < len(runes) {
					tokens[string(runes[i:i+2])] = struct{}{}
				}
			}
			continue
		}
		if len(runes) >= 2 {
			tokens[segment] = struct{}{}
		}
	}
	return tokens
}

func rankMemoryCandidates(items []memoryCandidate, query string, limit int) []memoryCandidate {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	queryTokens := memoryQueryTokens(query)
	if len(queryTokens) == 0 {
		return items[:limit]
	}
	type scored struct {
		item  memoryCandidate
		score int
		index int
	}
	ranked := make([]scored, 0, len(items))
	for index, item := range items {
		docTokens := memoryQueryTokens(item.Category + " " + item.Content)
		score := 0
		for token := range queryTokens {
			if _, ok := docTokens[token]; ok {
				score++
			}
		}
		ranked = append(ranked, scored{item: item, score: score, index: index})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if ranked[i].item.Importance != ranked[j].item.Importance {
			return ranked[i].item.Importance > ranked[j].item.Importance
		}
		return ranked[i].index < ranked[j].index
	})
	// If nothing matched, keep the pre-ranked (importance/recency) order.
	if ranked[0].score == 0 {
		return items[:limit]
	}
	out := make([]memoryCandidate, 0, limit)
	for _, entry := range ranked {
		if len(out) >= limit {
			break
		}
		out = append(out, entry.item)
	}
	return out
}
