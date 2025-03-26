package main

var AllStances = []Stance{
	// 🟢 Supportive / Agreeing
	{Type: "supportive", SubType: "strong_agreement", Summary: "Full support, clear siding with OP."},
	{Type: "supportive", SubType: "qualified_agreement", Summary: "Mostly agrees but points out a small flaw."},
	{Type: "supportive", SubType: "empathetic_support", Summary: "Offers emotional validation and comfort."},
	{Type: "supportive", SubType: "personal_anecdote_support", Summary: "Shares a similar experience to affirm OP."},

	// 🔴 Opposing / Critical
	{Type: "opposing", SubType: "direct_opposition", Summary: "Strong disagreement with OP’s actions or views."},
	{Type: "opposing", SubType: "blame_shifting", Summary: "Redirects blame to OP even if they don't see it."},
	{Type: "opposing", SubType: "moral_critique", Summary: "Argues from ethical grounds against OP."},
	{Type: "opposing", SubType: "logical_critique", Summary: "Breaks down inconsistencies or irrationality."},
	{Type: "opposing", SubType: "assumes_missing_context", Summary: "Suggests OP left out key info to make themselves look better."},

	// ⚪ Neutral / Analytical
	{Type: "neutral", SubType: "dispassionate_analysis", Summary: "Lays out facts without judgment."},
	{Type: "neutral", SubType: "devils_advocate", Summary: "Takes a contrary position just to explore it."},
	{Type: "neutral", SubType: "both_sides", Summary: "Sees nuance and avoids strong alignment."},
	{Type: "neutral", SubType: "not_enough_info", Summary: "Requests clarification or additional details before weighing in."},
	{Type: "neutral", SubType: "legal_perspective", Summary: "Discusses legality rather than morality or emotions."},

	// 🟡 Complex / Mixed
	{Type: "mixed", SubType: "its_complicated", Summary: "Sees conflicting truths; not easily resolved."},
	{Type: "mixed", SubType: "everyone_at_fault", Summary: "Points to multiple parties being wrong."},
	{Type: "mixed", SubType: "no_one_at_fault", Summary: "Sees it as a tragic or inevitable situation."},
	{Type: "mixed", SubType: "consequentialist_view", Summary: "Focuses on outcomes, not intent."},
	{Type: "mixed", SubType: "cultural_context", Summary: "Cites how cultural norms affect judgment."},

	// 🟣 Narrative / Relational
	{Type: "narrative", SubType: "neutral_anecdote", Summary: "Shares experience without clear judgment."},
	{Type: "narrative", SubType: "projective_comment", Summary: "Relates deeply and interprets through their own lens."},
	{Type: "narrative", SubType: "advice_giver", Summary: "Offers next steps or solutions instead of judgment."},
	{Type: "narrative", SubType: "therapist_style", Summary: "Gently reframes the situation to promote self-awareness."},

	// 🔵 Meta / Humor / Offbeat
	{Type: "meta", SubType: "snarky", Summary: "Uses humor or irony to criticize."},
	{Type: "meta", SubType: "meme_comment", Summary: "Light-hearted or playful, not serious."},
	{Type: "meta", SubType: "call_out_subreddit", Summary: "Comments on how typical or cliché the post is."},
	{Type: "meta", SubType: "structure_commentary", Summary: "Critiques how the post is written or what it omits."},
}
