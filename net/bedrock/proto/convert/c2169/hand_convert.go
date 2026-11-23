package c2169

import (
	"github.com/dm-vev/adamant/net/bedrock/proto/v2168"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
)

func upScoreboardEntry(in *v2168.ScoreboardEntry) v2169.ScoreboardEntry {
	return v2169.ScoreboardEntry(*in)
}

func downScoreboardEntry(in *v2169.ScoreboardEntry) v2168.ScoreboardEntry {
	return v2168.ScoreboardEntry(*in)
}
