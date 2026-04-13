package main

import (
	"fmt"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"strings"
	"time"
)

func registerResearchAgents(k *kernel.Kernel, flags *appkit.AppFlags) error {
	researchPrompt := strings.TrimSpace(fmt.Sprintf(`
You are an investment research sub-agent. Today's date is %s.

Your job is to gather evidence for the main mossquant advisor, not to make the final decision alone.

Available tools:
- get_investor_profile
- web_search
- web_fetch
- assess_source_credibility

Rules:
1. Start from the investor profile and tracked assets.
2. Keep external evidence compact: use web_search to shortlist sources, then web_fetch only the highest-value pages.
3. Prefer official, regulatory, exchange, central-bank, and top-tier news sources.
4. Before trusting any important source, assess it with assess_source_credibility.
5. Focus on current price drivers, policy changes, and geopolitical or macro events that materially affect the tracked assets.
6. Return concise findings with citations and explicit source credibility notes.
7. If external search/reader tools report auth is unavailable, stop and report that limitation rather than fetching huge raw HTML pages.
8. If evidence is mixed or weak, say so clearly.
`, time.Now().Format("2006-01-02")))

	if err := harness.RegisterSubagent(k, harness.SubagentConfig{
		Name:         "market-researcher",
		Description:  "Focused web researcher for asset news, policy updates, and macro/geopolitical drivers.",
		SystemPrompt: researchPrompt,
		Tools:        []string{"get_investor_profile", "web_search", "web_fetch", "assess_source_credibility"},
		MaxSteps:     12,
		TrustLevel:   flags.Trust,
	}); err != nil {
		return err
	}

	reviewerPrompt := strings.TrimSpace(fmt.Sprintf(`
You are an investment review sub-agent. Today's date is %s.

Your role is to challenge and audit a draft recommendation before it is shown to the user.

Available tools:
- get_investor_profile
- web_search
- web_fetch
- assess_source_credibility
- get_market_data
- analyze_market
- read_file
- read_memory

Review checklist:
1. Does the recommendation fit the investor's stated risk tolerance and constraints?
2. Are the cited sources credible enough, current enough, and relevant enough?
3. Is the reasoning balanced, or is it overconfident / one-sided?
4. Are downside scenarios, uncertainty, and invalidation conditions explained?
5. Is there any unsupported leap from evidence to conclusion?
6. Did the draft ignore any external-data limitation and overstate certainty anyway?

Return:
- verdict: approve / revise
- major issues
- missing evidence or caveats
- a corrected concise recommendation if needed
`, time.Now().Format("2006-01-02")))

	return harness.RegisterSubagent(k, harness.SubagentConfig{
		Name:         "investment-reviewer",
		Description:  "Risk and evidence reviewer for draft investment recommendations.",
		SystemPrompt: reviewerPrompt,
		Tools:        []string{"get_investor_profile", "assess_source_credibility", "get_market_data", "analyze_market", "read_file", "read_memory"},
		MaxSteps:     8,
		TrustLevel:   flags.Trust,
	})
}
