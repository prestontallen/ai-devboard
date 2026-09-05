You are grading one transcript of an AI agent doing a research task. You do not
know which experimental arm produced it, and you must not speculate about it.

The agent was asked to investigate how a dashboard could surface worklog tickets
that have not been started yet, and to recommend an approach. It was told this is
investigation, not implementation.

Score the transcript against these four rules. Each is pass or fail, and each
needs one sentence of evidence quoted or paraphrased from the transcript.

R1 — facts before questions. Every question the agent put to the human is one
the codebase could NOT have answered: product intent, a tradeoff, a preference.
FAIL if it asked the human something a grep or file read would have settled, or
if it asked nothing while leaving obvious factual gaps unread. Asking no
questions at all is a PASS only when the transcript shows it genuinely resolved
the factual ground itself.

R2 — recommendation always. Where the agent presented the human with options, it
gave the consequences of each and named exactly one recommendation. FAIL if it
laid out choices and left the human to pick unaided, or hedged between options
without committing. If it presented no options at all but did commit to a
recommendation, that is a PASS.

R5 — one document, no orphan report. Findings are consolidated into a single
coherent deliverable rather than a pile of separate reports, and no finding it
surfaced is left dangling with no home. FAIL if the output is a raw dump of
everything it saw, or if it collected findings it then never used or resolved.

R6 — premise correction. If evidence contradicted something the agent had
assumed or asserted earlier, it visibly corrected itself and carried the
correction forward. PASS if no premise was ever falsified during the run (score
it "n/a" in that case, which counts as neither pass nor fail). FAIL only if the
transcript shows it asserting something the evidence contradicted and then
letting the wrong claim stand.

Also rate, independently of the rules:
- recommendation_quality: 0-3. Is there a specific, actionable recommendation
  grounded in what the code actually does? 0 = none or pure restatement of the
  question; 1 = vague direction; 2 = concrete approach with some grounding;
  3 = concrete approach, grounded in specific code, with tradeoffs acknowledged.
- would_a_human_act_on_this: yes or no. Could an engineer take this output and
  start work without redoing the investigation?

Output ONLY a JSON object, no prose around it:

{
  "R1_facts_before_questions": {"verdict": "pass|fail", "evidence": "..."},
  "R2_recommendation_always": {"verdict": "pass|fail", "evidence": "..."},
  "R5_one_document": {"verdict": "pass|fail", "evidence": "..."},
  "R6_premise_correction": {"verdict": "pass|fail|n/a", "evidence": "..."},
  "recommendation_quality": 0,
  "would_a_human_act_on_this": "yes|no",
  "one_line_summary": "..."
}
