"""
Typesafe Integration Tests - Decisions

Exercises Bifrost's Typesafe surfaces over plain HTTP:
- POST /typesafe/v1/systemone: the 1:1 native drop-in (questions use `type`)
- GET  /typesafe/v1/models: native model listing synthesized from the datasheet
- POST /v1/decisions: Bifrost's unified decision format (questions use `kind`)

Covered scenarios:
1. Native systemone with all three question types (noul, choice, score)
2. Native model listing shape ({"models": [{"name", ...}]}, bare names)
3. Unified /v1/decisions with all three kinds and normalized answers
4. Native error shape on an invalid question type ({"detail": {...}})
5. Unified 400 on an unsupported kind
"""

import requests

from .utils.common import get_bifrost_base_url

STATE = (
    "Customer message: I was double charged last month and nobody replied "
    "to my two emails. I want a refund today or I am cancelling."
)

NATIVE_QUESTIONS = {
    "is_frustrated": {"type": "noul", "instructions": "Is the customer frustrated?"},
    "category": {
        "type": "choice",
        "instructions": "Pick the ticket category",
        "criteria": {"billing": "charges and refunds", "bug": "product defects", "other": "anything else"},
    },
    "urgency": {
        "type": "score",
        "instructions": "Rate how urgently this needs a human reply",
        "criteria": ["can wait a week", "should be answered soon", "needs a reply today"],
    },
}


class TestTypesafeNative:
    """POST /typesafe/v1/systemone and GET /typesafe/v1/models - native drop-in."""

    def test_01_systemone_all_question_types(self):
        response = requests.post(
            f"{get_bifrost_base_url()}/typesafe/v1/systemone",
            json={"state": STATE, "model": "jev-1.13.0", "questions": NATIVE_QUESTIONS},
            timeout=60,
        )
        assert response.status_code == 200, response.text
        body = response.json()

        assert body["model"] == "jev-1.13.0"
        answers = body["answers"]
        assert answers["is_frustrated"]["type"] == "noul"
        assert 0.0 <= answers["is_frustrated"]["noul"] <= 1.0
        assert answers["category"]["type"] == "choice"
        assert answers["category"]["choice"] in {"billing", "bug", "other"}
        assert answers["urgency"]["type"] == "score"
        assert isinstance(answers["urgency"]["score"], (int, float))
        assert body["usage"]["input_tokens"] > 0

    def test_02_native_models_shape(self):
        response = requests.get(f"{get_bifrost_base_url()}/typesafe/v1/models", timeout=30)
        assert response.status_code == 200, response.text
        names = [m["name"] for m in response.json()["models"]]
        assert "jev-1.13.0" in names
        assert "jev-latest" in names
        assert all("typesafe/" not in name for name in names)

    def test_03_native_error_shape(self):
        response = requests.post(
            f"{get_bifrost_base_url()}/typesafe/v1/systemone",
            json={
                "state": STATE,
                "model": "jev-1.13.0",
                "questions": {"q": {"type": "ranking", "instructions": "rank it"}},
            },
            timeout=30,
        )
        assert response.status_code == 400, response.text
        detail = response.json()["detail"]
        assert detail["message"]
        assert detail["error_type"]


class TestTypesafeDecisions:
    """POST /v1/decisions - Bifrost's unified decision format."""

    def test_01_decisions_all_kinds(self):
        questions = {
            name: {**{k: v for k, v in q.items() if k != "type"}, "kind": q["type"]} for name, q in NATIVE_QUESTIONS.items()
        }
        response = requests.post(
            f"{get_bifrost_base_url()}/v1/decisions",
            json={"model": "typesafe/jev-1.13.0", "state": STATE, "questions": questions},
            timeout=60,
        )
        assert response.status_code == 200, response.text
        body = response.json()

        answers = body["answers"]
        assert answers["is_frustrated"]["kind"] == "noul"
        assert 0.0 <= answers["is_frustrated"]["value"] <= 1.0
        assert answers["category"]["kind"] == "choice"
        assert answers["category"]["value"] in {"billing", "bug", "other"}
        assert answers["urgency"]["kind"] == "score"
        assert isinstance(answers["urgency"]["value"], (int, float))
        assert body["usage"]["prompt_tokens"] > 0

    def test_02_unsupported_kind_rejected(self):
        response = requests.post(
            f"{get_bifrost_base_url()}/v1/decisions",
            json={
                "model": "typesafe/jev-1.13.0",
                "state": STATE,
                "questions": {"q": {"kind": "ranking", "instructions": "rank it"}},
            },
            timeout=30,
        )
        assert response.status_code == 400, response.text
        assert "unsupported kind" in response.text
