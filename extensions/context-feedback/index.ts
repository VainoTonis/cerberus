/**
 * Context Feedback Extension
 *
 * A bundled base extension that inspects user input for a complete,
 * non-empty <orchestrator-context>...</orchestrator-context> block.
 *
 * When such a block is present, this extension activates and returns
 * host-mediated context quality feedback directly (no LLM call, no
 * network access, no filesystem access, no vault access). The feedback
 * is a deterministic, local assessment of the supplied context block:
 *
 *   - verdict: "useful" | "insufficient" | "irrelevant" | "conflicting"
 *   - reason: short human-readable explanation
 *   - confidence: "low" | "medium" | "high"
 *
 * Any input that does not contain a complete, non-empty
 * <orchestrator-context> block passes through unchanged.
 */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

type Verdict = "useful" | "insufficient" | "irrelevant" | "conflicting";
type Confidence = "low" | "medium" | "high";

interface ContextFeedback {
	verdict: Verdict;
	reason: string;
	confidence: Confidence;
}

const ORCHESTRATOR_CONTEXT_RE = /<orchestrator-context>([\s\S]*?)<\/orchestrator-context>/i;

const CONFLICT_MARKERS = [
	"conflicting information",
	"contradicts",
	"contradiction",
	"inconsistent with",
	"mutually exclusive",
];

const IRRELEVANT_MARKERS = [
	"not related",
	"unrelated",
	"off-topic",
	"off topic",
	"irrelevant",
	"no bearing on",
	"does not apply",
];

const INSUFFICIENT_MARKERS = [
	"todo",
	"tbd",
	"unknown",
	"n/a",
	"not specified",
	"unspecified",
	"unclear",
	"no details",
];

/** Extracts a complete <orchestrator-context> block, if present and non-empty. */
function extractOrchestratorContext(text: string): string | undefined {
	const match = ORCHESTRATOR_CONTEXT_RE.exec(text);
	if (!match) return undefined;
	const content = match[1]?.trim();
	if (!content) return undefined;
	return content;
}

function countMarkers(haystack: string, markers: string[]): number {
	return markers.reduce((count, marker) => (haystack.includes(marker) ? count + 1 : count), 0);
}

/** Deterministic, host-local quality assessment of a context block. */
function evaluateContext(content: string): ContextFeedback {
	const lower = content.toLowerCase();
	const wordCount = content.split(/\s+/).filter(Boolean).length;

	const conflictHits = countMarkers(lower, CONFLICT_MARKERS);
	if (conflictHits > 0) {
		return {
			verdict: "conflicting",
			reason: "Context explicitly describes contradictory or mutually exclusive information",
			confidence: conflictHits > 1 ? "high" : "medium",
		};
	}

	const irrelevantHits = countMarkers(lower, IRRELEVANT_MARKERS);
	if (irrelevantHits > 0) {
		return {
			verdict: "irrelevant",
			reason: "Context indicates it does not relate to the task at hand",
			confidence: irrelevantHits > 1 ? "high" : "medium",
		};
	}

	const insufficientHits = countMarkers(lower, INSUFFICIENT_MARKERS);
	const tooShort = wordCount < 8;
	if (insufficientHits > 0 || tooShort) {
		return {
			verdict: "insufficient",
			reason: tooShort
				? "Context is too short to provide actionable detail"
				: "Context contains placeholders or unresolved gaps",
			confidence: tooShort && insufficientHits > 0 ? "high" : "medium",
		};
	}

	const richness = countMarkers(lower, ["because", "specifically", "for example", "e.g.", "step"]);
	const confidence: Confidence = wordCount > 60 && richness > 0 ? "high" : wordCount > 20 ? "medium" : "low";

	return {
		verdict: "useful",
		reason: "Context is specific, on-topic, and internally consistent",
		confidence,
	};
}

export default function (pi: ExtensionAPI) {
	pi.on("input", async (event, ctx) => {
		const context = extractOrchestratorContext(event.text);
		if (context === undefined) {
			return { action: "continue" };
		}

		const feedback = evaluateContext(context);

		// Host-mediated feedback: computed locally and returned directly to the
		// caller via the notification channel, without invoking the model.
		ctx.ui.notify(JSON.stringify(feedback), "info");

		return { action: "handled" };
	});
}
