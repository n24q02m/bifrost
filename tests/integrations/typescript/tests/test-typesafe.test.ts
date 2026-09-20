/**
 * Typesafe Integration Tests - Decisions
 *
 * Exercises Bifrost's Typesafe surfaces over plain HTTP:
 * - POST /typesafe/v1/systemone: the 1:1 native drop-in (questions use `type`)
 * - GET  /typesafe/v1/models: native model listing synthesized from the datasheet
 * - POST /v1/decisions: Bifrost's unified decision format (questions use `kind`)
 */

import { describe, expect, it } from "vitest";

const baseUrl = process.env.BIFROST_BASE_URL || "http://localhost:8080";

const STATE =
	"Customer message: I was double charged last month and nobody replied to my two emails. I want a refund today or I am cancelling.";

const NATIVE_QUESTIONS = {
	is_frustrated: { type: "noul", instructions: "Is the customer frustrated?" },
	category: {
		type: "choice",
		instructions: "Pick the ticket category",
		criteria: { billing: "charges and refunds", bug: "product defects", other: "anything else" },
	},
	urgency: {
		type: "score",
		instructions: "Rate how urgently this needs a human reply",
		criteria: ["can wait a week", "should be answered soon", "needs a reply today"],
	},
};

describe("Typesafe native drop-in", () => {
	it("systemone answers all three question types", async () => {
		const response = await fetch(`${baseUrl}/typesafe/v1/systemone`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ state: STATE, model: "jev-1.13.0", questions: NATIVE_QUESTIONS }),
		});
		const body = await response.json();
		expect(response.status, JSON.stringify(body)).toBe(200);

		expect(body.model).toBe("jev-1.13.0");
		expect(body.answers.is_frustrated.type).toBe("noul");
		expect(body.answers.is_frustrated.noul).toBeGreaterThanOrEqual(0);
		expect(body.answers.is_frustrated.noul).toBeLessThanOrEqual(1);
		expect(body.answers.category.type).toBe("choice");
		expect(["billing", "bug", "other"]).toContain(body.answers.category.choice);
		expect(body.answers.urgency.type).toBe("score");
		expect(typeof body.answers.urgency.score).toBe("number");
		expect(body.usage.input_tokens).toBeGreaterThan(0);
	}, 60000);

	it("native models listing uses bare jev names", async () => {
		const response = await fetch(`${baseUrl}/typesafe/v1/models`);
		const body = await response.json();
		expect(response.status, JSON.stringify(body)).toBe(200);
		const names = body.models.map((m: { name: string }) => m.name);
		expect(names).toContain("jev-1.13.0");
		expect(names).toContain("jev-latest");
		for (const name of names) expect(name).not.toContain("typesafe/");
	}, 30000);

	it("errors come back in the native detail shape", async () => {
		const response = await fetch(`${baseUrl}/typesafe/v1/systemone`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				state: STATE,
				model: "jev-1.13.0",
				questions: { q: { type: "ranking", instructions: "rank it" } },
			}),
		});
		expect(response.status).toBe(400);
		const body = await response.json();
		expect(body.detail.message).toBeTruthy();
		expect(body.detail.error_type).toBeTruthy();
	}, 30000);
});

describe("Typesafe unified /v1/decisions", () => {
	it("answers all three kinds with normalized values", async () => {
		const questions = Object.fromEntries(
			Object.entries(NATIVE_QUESTIONS).map(([name, q]) => {
				const { type, ...rest } = q as { type: string } & Record<string, unknown>;
				return [name, { ...rest, kind: type }];
			}),
		);
		const response = await fetch(`${baseUrl}/v1/decisions`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ model: "typesafe/jev-1.13.0", state: STATE, questions }),
		});
		const body = await response.json();
		expect(response.status, JSON.stringify(body)).toBe(200);

		expect(body.answers.is_frustrated.kind).toBe("noul");
		expect(body.answers.is_frustrated.value).toBeGreaterThanOrEqual(0);
		expect(body.answers.is_frustrated.value).toBeLessThanOrEqual(1);
		expect(["billing", "bug", "other"]).toContain(body.answers.category.value);
		expect(typeof body.answers.urgency.value).toBe("number");
		expect(body.usage.prompt_tokens).toBeGreaterThan(0);
	}, 60000);

	it("rejects an unsupported kind with 400", async () => {
		const response = await fetch(`${baseUrl}/v1/decisions`, {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({
				model: "typesafe/jev-1.13.0",
				state: STATE,
				questions: { q: { kind: "ranking", instructions: "rank it" } },
			}),
		});
		expect(response.status).toBe(400);
		expect(await response.text()).toContain("unsupported kind");
	}, 30000);
});
