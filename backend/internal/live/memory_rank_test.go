package live

import "testing"

func TestRankMemoryCandidatesPrefersQueryOverlap(t *testing.T) {
	items := []memoryCandidate{
		{Category: "profile", Content: "我喜欢喝咖啡", Importance: 5},
		{Category: "profile", Content: "我的部署窗口是每周五 18:00", Importance: 1},
		{Category: "profile", Content: "我住在上海", Importance: 4},
	}
	ranked := rankMemoryCandidates(items, "部署窗口是什么时候", 2)
	if len(ranked) != 2 || ranked[0].Content != "我的部署窗口是每周五 18:00" {
		t.Fatalf("unexpected ranking: %#v", ranked)
	}
}
