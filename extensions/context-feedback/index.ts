/**
 * Context Feedback Extension
 *
 * Registers `report_context_feedback`, a tool the agent calls itself, as its
 * final action, to submit a universal feedback report for the current
 * subagent turn.
 *
 * This report covers the task outcome, task clarity, and the quality of any
 * <orchestrator-context>...</orchestrator-context> block that may have been
 * supplied with the prompt. The report is required on every subagent turn,
 * regardless of whether an orchestrator-context block was present.
 *
 * This extension does not assess anything itself. It never inspects "input"
 * events, never cancels tasks, never heuristically judges the task or
 * context, and never notifies the UI. Its only job is:
 *
 *   1. On `before_agent_start`, activate `report_context_feedback` for this
 *      turn and inject a single instruction telling the agent to call it
 *      exactly once, as its final action, with its own honest assessment.
 *   2. Detect whether the prompt contains a complete, non-empty
 *      <orchestrator-context> block, and tailor the injected instruction so
 *      the agent knows whether context was supplied or not (feedback is
 *      still required either way).
 *
 * The tool itself simply records the agent-authored report in its durable
 * result `details` and terminates the turn.
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
	taskOutcome: "success" | "partial" | "failure";
	taskClarity: "clear" | "unclear";
	contextQuality: "useful" | "insufficient" | "irrelevant" | "conflicting" | "not_supplied";
	reason: string;
	suggestion?: string;
	confidence: "low" | "medium" | "high";
}

const reportContextFeedbackTool = defineTool({
	name: TOOL_NAME,
	label: "Report Context Feedback",
	description:
		"Submit your final universal feedback report for this turn: task outcome, task clarity, " +
		"context quality (use 'not_supplied' if no orchestrator-context block was given), reason, " +
		"an optional suggestion, and your confidence. Call this exactly once, as your last action.",
	promptSnippet: "Submit your final universal feedback report for this turn",
	promptGuidelines: [
		"Call report_context_feedback exactly once, as your final action, on every turn — whether or not an orchestrator-context block was supplied.",
		"Base taskOutcome, taskClarity, contextQuality, reason, suggestion, and confidence on your own honest assessment, not a heuristic.",
		"If no orchestrator-context block was supplied, set contextQuality to 'not_supplied'.",
	],
	parameters: Type.Object({
		taskOutcome: StringEnum(["success", "partial", "failure"] as const, {
			description: "Your assessment of how the task went: success, partial, or failure.",
		}),
		taskClarity: StringEnum(["clear", "unclear"] as const, {
			description: "Whether the task instructions you were given were clear or unclear.",
		}),
		contextQuality: StringEnum(
			["useful", "insufficient", "irrelevant", "conflicting", "not_supplied"] as const,
			{
				description:
					"Your assessment of the supplied orchestrator-context block: useful, insufficient, " +
					"irrelevant, or conflicting. Use 'not_supplied' if no such block was given for this turn.",
			},
		),
		reason: Type.String({
			description: "Short human-readable explanation for the taskOutcome/taskClarity/contextQuality assessments.",
		}),
		suggestion: Type.Optional(
			Type.String({
				description: "Optional suggestion for how the task or context could be improved next time.",
			}),
		),
		confidence: StringEnum(["low", "medium", "high"] as const, {
			description: "Your confidence in this report: low, medium, or high.",
		}),
	}),

	async execute(_toolCallId, params) {
		const details: ContextFeedbackDetails = {
			taskOutcome: params.taskOutcome,
			taskClarity: params.taskClarity,
			contextQuality: params.contextQuality,
			reason: params.reason,
			...(params.suggestion ? { suggestion: params.suggestion } : {}),
			confidence: params.confidence,
		};

		return {
			content: [
				{
					type: "text",
					text:
						`Feedback recorded: outcome=${params.taskOutcome}, clarity=${params.taskClarity}, ` +
						`context=${params.contextQuality} (${params.confidence} confidence) — ${params.reason}`,
				},
			],
			details,
			terminate: true,
		};
	},
});

export default function (pi: ExtensionAPI) {
	pi.registerTool(reportContextFeedbackTool);

	// Keep the tool registered but inactive by default until a turn starts.
	pi.on("session_start", () => {
		const initialTools = pi.getActiveTools().filter((name) => name !== TOOL_NAME);
		pi.setActiveTools(initialTools);
	});

	pi.on("before_agent_start", async (event) => {
		const active = pi.getActiveTools();
		if (!active.includes(TOOL_NAME)) {
			pi.setActiveTools([...active, TOOL_NAME]);
		}

		const contextSupplied = hasCompleteNonEmptyOrchestratorContext(event.prompt);

		return {
			message: {
				customType: "context-feedback-instruction",
				content: contextSupplied
					? "An <orchestrator-context> block was supplied with this prompt. Before finishing, call " +
						"report_context_feedback exactly once, as your final action, with your own honest " +
						"universal feedback report: task outcome, task clarity, context quality, reason, an " +
						"optional suggestion, and your confidence."
					: "No <orchestrator-context> block was supplied with this prompt. Before finishing, call " +
						"report_context_feedback exactly once, as your final action, with your own honest " +
						"universal feedback report: task outcome, task clarity, contextQuality set to " +
						"'not_supplied', reason, an optional suggestion, and your confidence.",
				display: false,
			},
		};
	});
}
