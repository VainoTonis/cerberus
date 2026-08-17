/**
 * Context Feedback Extension
 *
 * Registers `report_context_feedback`, a tool the agent calls itself, as its
 * final action, to report its own assessment of the quality of an
 * <orchestrator-context>...</orchestrator-context> block that was supplied
 * with the current prompt.
 *
 * This extension does not assess context quality itself. It never inspects
 * "input" events, never cancels tasks, never heuristically judges the
 * context, and never notifies the UI. Its only job is:
 *
 *   1. On `before_agent_start`, check whether the prompt contains a
 *      complete, non-empty <orchestrator-context> block.
 *   2. If so, activate `report_context_feedback` for this turn and inject a
 *      single instruction telling the agent to call it exactly once, as its
 *      final action, with its own honest assessment.
 *   3. If not, do nothing — the prompt passes through completely unchanged.
 *
 * The tool itself simply records the agent-authored verdict, reason, and
 * confidence in its durable result `details` and terminates the turn.
 */
import { defineTool, type ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Type } from "typebox";

const TOOL_NAME = "report_context_feedback";

const ORCHESTRATOR_CONTEXT_RE = /<orchestrator-context>([\s\S]*?)<\/orchestrator-context>/i;

/** Returns true only if the prompt contains a complete, non-empty block. */
function hasCompleteNonEmptyOrchestratorContext(text: string): boolean {
	const match = ORCHESTRATOR_CONTEXT_RE.exec(text);
	if (!match) return false;
	const content = match[1]?.trim();
	return Boolean(content);
}

interface ContextFeedbackDetails {
	verdict: "useful" | "insufficient" | "irrelevant" | "conflicting";
	reason: string;
	confidence: "low" | "medium" | "high";
}

const reportContextFeedbackTool = defineTool({
	name: TOOL_NAME,
	label: "Report Context Feedback",
	description:
		"Report your own final assessment of the quality of the supplied orchestrator-context block. " +
		"Call this exactly once, as your last action, after you have finished using the context.",
	promptSnippet: "Report your final assessment of the supplied orchestrator-context block",
	promptGuidelines: [
		"Call report_context_feedback exactly once, as your final action, when an orchestrator-context block was supplied for this turn.",
		"Base the report_context_feedback verdict, reason, and confidence on your own honest assessment, not a heuristic.",
	],
	parameters: Type.Object({
		verdict: StringEnum(["useful", "insufficient", "irrelevant", "conflicting"] as const, {
			description:
				"Your assessment of the supplied context: useful, insufficient, irrelevant, or conflicting.",
		}),
		reason: Type.String({ description: "Short human-readable explanation for the verdict." }),
		confidence: StringEnum(["low", "medium", "high"] as const, {
			description: "Your confidence in this verdict: low, medium, or high.",
		}),
	}),

	async execute(_toolCallId, params) {
		const details: ContextFeedbackDetails = {
			verdict: params.verdict,
			reason: params.reason,
			confidence: params.confidence,
		};

		return {
			content: [
				{
					type: "text",
					text: `Context feedback recorded: ${params.verdict} (${params.confidence} confidence) — ${params.reason}`,
				},
			],
			details,
			terminate: true,
		};
	},
});

export default function (pi: ExtensionAPI) {
	pi.registerTool(reportContextFeedbackTool);

	// Keep the tool registered but inactive by default; unmarked tasks are
	// left completely unchanged.
	pi.on("session_start", () => {
		const initialTools = pi.getActiveTools().filter((name) => name !== TOOL_NAME);
		pi.setActiveTools(initialTools);
	});

	pi.on("before_agent_start", async (event) => {
		if (!hasCompleteNonEmptyOrchestratorContext(event.prompt)) {
			return;
		}

		const active = pi.getActiveTools();
		if (!active.includes(TOOL_NAME)) {
			pi.setActiveTools([...active, TOOL_NAME]);
		}

		return {
			message: {
				customType: "context-feedback-instruction",
				content:
					"An <orchestrator-context> block was supplied with this prompt. After you finish using it, " +
					"call report_context_feedback exactly once, as your final action, with your own honest " +
					"assessment (verdict, reason, confidence) of its quality.",
				display: false,
			},
		};
	});
}
