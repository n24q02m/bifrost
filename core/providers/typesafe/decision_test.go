package typesafe

import (
	"reflect"
	"strings"
	"testing"

	schemas "github.com/maximhq/bifrost/core/schemas"
)

// decisionRequest builds a BifrostDecisionRequest around the given questions.
func decisionRequest(state interface{}, questions map[string]schemas.DecisionQuestion) *schemas.BifrostDecisionRequest {
	return &schemas.BifrostDecisionRequest{
		Provider:  schemas.Typesafe,
		Model:     "jev-1.13.0",
		State:     state,
		Questions: questions,
	}
}

func TestToTypesafeDecisionRequestMixedKinds(t *testing.T) {
	req := decisionRequest("the user asked for a refund", map[string]schemas.DecisionQuestion{
		"is_angry": {
			Kind:         schemas.DecisionKindNoul,
			Instructions: "Is the user angry?",
		},
		"category": {
			Kind:         schemas.DecisionKindChoice,
			Instructions: "Pick the ticket category",
			Criteria:     map[string]interface{}{"billing": "money issues", "bug": "product defects", "other": "anything else"},
		},
		"severity": {
			Kind:         schemas.DecisionKindScore,
			Instructions: "Rate the severity",
			Criteria:     []interface{}{"cosmetic", "annoying", "blocking"},
		},
	})

	native, err := ToTypesafeDecisionRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if native.Model != "jev-1.13.0" {
		t.Errorf("model = %q, want bare jev-1.13.0", native.Model)
	}
	if native.State != "the user asked for a refund" {
		t.Errorf("state not preserved: %v", native.State)
	}
	if len(native.Questions) != 3 {
		t.Fatalf("expected 3 questions, got %d", len(native.Questions))
	}

	angry := native.Questions["is_angry"]
	if angry.Type != TypesafeQuestionTypeNoul {
		t.Errorf("is_angry type = %q, want noul", angry.Type)
	}
	if angry.Instructions != "Is the user angry?" {
		t.Errorf("is_angry instructions = %v", angry.Instructions)
	}

	category := native.Questions["category"]
	if category.Type != TypesafeQuestionTypeChoice {
		t.Errorf("category type = %q, want choice", category.Type)
	}
	criteria, ok := category.Criteria.(map[string]string)
	if !ok || len(criteria) != 3 || criteria["bug"] != "product defects" {
		t.Errorf("category criteria not preserved: %#v", category.Criteria)
	}

	severity := native.Questions["severity"]
	if severity.Type != TypesafeQuestionTypeScore {
		t.Errorf("severity type = %q, want score", severity.Type)
	}
	levels, ok := severity.Criteria.([]interface{})
	if !ok || len(levels) != 3 || levels[0] != "cosmetic" {
		t.Errorf("severity criteria not preserved losslessly: %#v", severity.Criteria)
	}
}

func TestToTypesafeDecisionRequestStructuredStateAndInstructions(t *testing.T) {
	state := map[string]interface{}{"ticket": map[string]interface{}{"id": 42, "body": "hello"}}
	structured := map[string]interface{}{"goal": "judge tone", "steps": []interface{}{"read", "decide"}}
	req := decisionRequest(state, map[string]schemas.DecisionQuestion{
		"tone_ok": {Kind: schemas.DecisionKindNoul, Instructions: structured},
	})

	native, err := ToTypesafeDecisionRequest(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(native.Questions["tone_ok"].Instructions, interface{}(structured)) {
		t.Errorf("structured instructions not lossless: %#v", native.Questions["tone_ok"].Instructions)
	}
	if !reflect.DeepEqual(native.State, interface{}(state)) {
		t.Errorf("structured state not lossless: %#v", native.State)
	}
}

func TestToTypesafeDecisionRequestRejections(t *testing.T) {
	cases := []struct {
		name     string
		question schemas.DecisionQuestion
		wantSub  string
	}{
		{
			name:     "unsupported kind",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKind("ranking"), Instructions: "d"},
			wantSub:  "unsupported kind",
		},
		{
			name:     "missing instructions",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindNoul},
			wantSub:  "no instructions",
		},
		{
			name:     "noul criteria with bad key",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindNoul, Instructions: "d", Criteria: map[string]interface{}{"maybe": "x"}},
			wantSub:  `allows only "true" and "false" keys`,
		},
		{
			name:     "choice without criteria",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindChoice, Instructions: "d"},
			wantSub:  "requires criteria options",
		},
		{
			name:     "choice criteria wrong shape",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindChoice, Instructions: "d", Criteria: []interface{}{"a", "b"}},
			wantSub:  "must be a map of descriptions",
		},
		{
			name:     "choice criteria non-string description",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindChoice, Instructions: "d", Criteria: map[string]interface{}{"a": 1}},
			wantSub:  "must be a string",
		},
		{
			name:     "score criteria missing",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindScore, Instructions: "d"},
			wantSub:  "ordered array",
		},
		{
			name:     "score criteria too short",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindScore, Instructions: "d", Criteria: []interface{}{"only one"}},
			wantSub:  "between 2 and 10",
		},
		{
			name:     "score criteria non-string level",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindScore, Instructions: "d", Criteria: []interface{}{"low", 2, "high"}},
			wantSub:  "level 1 must be a string",
		},
		{
			name:     "instructions with unsupported shape",
			question: schemas.DecisionQuestion{Kind: schemas.DecisionKindNoul, Instructions: 42},
			wantSub:  "instructions must be a string, object, or array",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := decisionRequest("state", map[string]schemas.DecisionQuestion{"q": tc.question})
			_, err := ToTypesafeDecisionRequest(req)
			if err == nil {
				t.Fatalf("expected rejection containing %q, got nil error", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}

	t.Run("state with unsupported shape", func(t *testing.T) {
		// Pointers to scalars marshal to JSON scalars; funcs cannot marshal at
		// all - both must be caller errors, not upstream 422s or marshal 500s.
		for _, bad := range []interface{}{true, 42, 3.14, nil, new(42), func() {}} {
			req := decisionRequest(bad, map[string]schemas.DecisionQuestion{
				"q": {Kind: schemas.DecisionKindNoul, Instructions: "d"},
			})
			_, err := ToTypesafeDecisionRequest(req)
			if err == nil || !strings.Contains(err.Error(), "state must be a string, object, or array") {
				t.Fatalf("expected state shape rejection for %T, got %v", bad, err)
			}
		}
	})

	t.Run("instructions validated by serialized shape", func(t *testing.T) {
		type rubric struct {
			Goal string `json:"goal"`
		}
		for _, good := range []interface{}{rubric{Goal: "judge tone"}, []string{"read", "decide"}, map[string]string{"goal": "judge"}} {
			req := decisionRequest("state", map[string]schemas.DecisionQuestion{
				"q": {Kind: schemas.DecisionKindNoul, Instructions: good},
			})
			if _, err := ToTypesafeDecisionRequest(req); err != nil {
				t.Fatalf("expected %T instructions to be accepted, got %v", good, err)
			}
		}
		for _, bad := range []interface{}{7, new(3.5), func() {}} {
			req := decisionRequest("state", map[string]schemas.DecisionQuestion{
				"q": {Kind: schemas.DecisionKindNoul, Instructions: bad},
			})
			_, err := ToTypesafeDecisionRequest(req)
			if err == nil || !strings.Contains(err.Error(), "instructions must be a string, object, or array") {
				t.Fatalf("expected instructions shape rejection for %T, got %v", bad, err)
			}
		}
	})

	t.Run("typed string slice score criteria accepted", func(t *testing.T) {
		req := decisionRequest("state", map[string]schemas.DecisionQuestion{
			"q": {Kind: schemas.DecisionKindScore, Instructions: "d", Criteria: []string{"low", "medium", "high"}},
		})
		native, err := ToTypesafeDecisionRequest(req)
		if err != nil {
			t.Fatalf("expected []string score criteria to be accepted, got %v", err)
		}
		levels, ok := native.Questions["q"].Criteria.([]interface{})
		if !ok || len(levels) != 3 || levels[0] != "low" {
			t.Fatalf("score criteria not normalized to ordered array: %#v", native.Questions["q"].Criteria)
		}
	})

	t.Run("typed maps, slices, and structs are valid shapes", func(t *testing.T) {
		// Go SDK callers pass typed values; anything serializing to a JSON
		// object or array satisfies the contract, not only the interface types
		// an HTTP JSON decode produces.
		type ticket struct {
			ID   int    `json:"id"`
			Body string `json:"body"`
		}
		for _, good := range []interface{}{
			map[string]string{"key": "value"},
			[]string{"a", "b"},
			ticket{ID: 1, Body: "hello"},
		} {
			req := decisionRequest(good, map[string]schemas.DecisionQuestion{
				"q": {Kind: schemas.DecisionKindNoul, Instructions: "d"},
			})
			if _, err := ToTypesafeDecisionRequest(req); err != nil {
				t.Fatalf("expected %T to be a valid state shape, got %v", good, err)
			}
		}
	})
}

func TestToBifrostDecisionResponseAllKindsWithZeroAndFractionalValues(t *testing.T) {
	req := decisionRequest("state", map[string]schemas.DecisionQuestion{
		"is_spam": {Kind: schemas.DecisionKindNoul, Instructions: "d"},
		"lang":    {Kind: schemas.DecisionKindChoice, Instructions: "d", Criteria: map[string]interface{}{"en": "", "de": ""}},
		"quality": {Kind: schemas.DecisionKindScore, Instructions: "d", Criteria: []interface{}{"bad", "ok", "good"}},
	})

	zero := 0.0
	fractional := 1.75
	confidence := 0.9
	lang := "de"
	native := &TypesafeDecisionResponse{
		Model: "jev-1.13.0",
		Answers: map[string]TypesafeAnswer{
			// Zero is a legitimate evaluated value and must survive.
			"is_spam": {Type: TypesafeQuestionTypeNoul, Noul: &zero, Probabilities: map[string]float64{"true": 0.0, "false": 1.0}},
			"lang":    {Type: TypesafeQuestionTypeChoice, Choice: &lang, Probabilities: map[string]float64{"en": 0.1, "de": 0.9}, Confidence: &confidence},
			"quality": {Type: TypesafeQuestionTypeScore, Score: &fractional, Legend: map[string]string{"1": "bad", "2": "ok", "3": "good"}},
		},
		Usage: &TypesafeUsage{InputTokens: 120, OutputTokens: 0},
	}

	resp, bifrostErr := ToBifrostDecisionResponse(native, req)
	if bifrostErr != nil {
		t.Fatalf("unexpected error: %v", bifrostErr)
	}
	if resp.Model != "jev-1.13.0" {
		t.Errorf("model = %q", resp.Model)
	}

	spam := resp.Answers["is_spam"]
	if spam.Kind != schemas.DecisionKindNoul || spam.Value != 0.0 {
		t.Errorf("zero-valued noul answer lost: %+v", spam)
	}
	if spam.Probabilities["false"] != 1.0 {
		t.Errorf("noul probabilities lost: %+v", spam.Probabilities)
	}

	langAnswer := resp.Answers["lang"]
	if langAnswer.Value != "de" {
		t.Errorf("choice value = %v", langAnswer.Value)
	}
	if langAnswer.Confidence == nil || *langAnswer.Confidence != 0.9 {
		t.Errorf("confidence lost: %+v", langAnswer)
	}

	quality := resp.Answers["quality"]
	if quality.Value != 1.75 {
		t.Errorf("fractional score lost: %v", quality.Value)
	}
	if quality.Legend["3"] != "good" {
		t.Errorf("legend lost: %+v", quality.Legend)
	}

	if resp.Usage == nil || resp.Usage.PromptTokens != 120 || resp.Usage.CompletionTokens != 0 || resp.Usage.TotalTokens != 120 {
		t.Errorf("usage not normalized: %+v", resp.Usage)
	}
}

func TestToBifrostDecisionResponseFailures(t *testing.T) {
	req := decisionRequest("state", map[string]schemas.DecisionQuestion{
		"is_spam": {Kind: schemas.DecisionKindNoul, Instructions: "d"},
	})

	t.Run("missing answer", func(t *testing.T) {
		native := &TypesafeDecisionResponse{Model: "jev-1.13.0", Answers: map[string]TypesafeAnswer{}}
		if _, err := ToBifrostDecisionResponse(native, req); err == nil {
			t.Fatal("expected error for missing answer")
		}
	})

	t.Run("answer type mismatch", func(t *testing.T) {
		choice := "yes"
		native := &TypesafeDecisionResponse{
			Model:   "jev-1.13.0",
			Answers: map[string]TypesafeAnswer{"is_spam": {Type: TypesafeQuestionTypeChoice, Choice: &choice}},
		}
		if _, err := ToBifrostDecisionResponse(native, req); err == nil {
			t.Fatal("expected error for mismatched answer type")
		}
	})

	t.Run("value missing for type", func(t *testing.T) {
		native := &TypesafeDecisionResponse{
			Model:   "jev-1.13.0",
			Answers: map[string]TypesafeAnswer{"is_spam": {Type: TypesafeQuestionTypeNoul}},
		}
		if _, err := ToBifrostDecisionResponse(native, req); err == nil {
			t.Fatal("expected error for noul answer without value")
		}
	})

	t.Run("noul value outside range", func(t *testing.T) {
		// The provider response is untrusted; a noul outside [0,1] violates the
		// documented contract and must not surface as a successful answer.
		for _, bad := range []float64{-0.2, 1.4} {
			value := bad
			native := &TypesafeDecisionResponse{
				Model:   "jev-1.13.0",
				Answers: map[string]TypesafeAnswer{"is_spam": {Type: TypesafeQuestionTypeNoul, Noul: &value}},
			}
			if _, err := ToBifrostDecisionResponse(native, req); err == nil {
				t.Fatalf("expected error for noul value %v outside [0,1]", bad)
			}
		}
	})
}

func TestNativeRoundTrip(t *testing.T) {
	native := &TypesafeDecisionRequest{
		State: "some state",
		Model: "jev-latest",
		Questions: map[string]TypesafeQuestion{
			"approve": {Type: TypesafeQuestionTypeNoul, Instructions: "Approve?", Criteria: map[string]interface{}{"true": "approve it", "false": "reject it"}},
			"bucket":  {Type: TypesafeQuestionTypeChoice, Instructions: "Bucket?", Criteria: map[string]interface{}{"a": "first", "b": "second"}},
			"rating":  {Type: TypesafeQuestionTypeScore, Instructions: "Rate it", Criteria: []interface{}{"low", "high"}},
		},
	}

	shared, err := native.ToBifrostDecisionRequest(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shared.Provider != schemas.Typesafe || shared.Model != "jev-latest" {
		t.Errorf("routing = %s/%s", shared.Provider, shared.Model)
	}
	if len(shared.Questions) != 3 {
		t.Fatalf("expected 3 questions, got %d", len(shared.Questions))
	}

	// The shared shape must convert straight back to the native shape.
	back, err := ToTypesafeDecisionRequest(shared)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	for name, question := range native.Questions {
		got, ok := back.Questions[name]
		if !ok {
			t.Fatalf("question %q lost in round trip", name)
		}
		if got.Type != question.Type {
			t.Errorf("question %q type = %q, want %q", name, got.Type, question.Type)
		}
	}

	// Explicit provider prefix routes the same way.
	native.Model = "typesafe/jev-1.13.0"
	shared, err = native.ToBifrostDecisionRequest(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shared.Provider != schemas.Typesafe || shared.Model != "jev-1.13.0" {
		t.Errorf("prefixed routing = %s/%s", shared.Provider, shared.Model)
	}
}

func TestToTypesafeNativeDecisionResponse(t *testing.T) {
	resp := &schemas.BifrostDecisionResponse{
		Model: "jev-1.13.0",
		Answers: map[string]schemas.DecisionAnswer{
			"approve": {Kind: schemas.DecisionKindNoul, Value: 0.25, Probabilities: map[string]float64{"true": 0.25, "false": 0.75}},
		},
		Usage: &schemas.BifrostLLMUsage{PromptTokens: 10, CompletionTokens: 0, TotalTokens: 10},
	}

	native, err := ToTypesafeNativeDecisionResponse(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	answer := native.Answers["approve"]
	if answer.Type != TypesafeQuestionTypeNoul || answer.Noul == nil || *answer.Noul != 0.25 {
		t.Errorf("noul answer not rebuilt: %+v", answer)
	}
	if answer.Probabilities["false"] != 0.75 {
		t.Errorf("probabilities lost: %+v", answer.Probabilities)
	}
	if native.Usage == nil || native.Usage.InputTokens != 10 {
		t.Errorf("usage lost: %+v", native.Usage)
	}
}

// noopLogger satisfies schemas.Logger for constructor calls in unit tests.
// schemas cannot provide one and core's DefaultLogger would import-cycle here.
type noopLogger struct{}

func (noopLogger) Debug(string, ...any)                                            {}
func (noopLogger) Info(string, ...any)                                             {}
func (noopLogger) Warn(string, ...any)                                             {}
func (noopLogger) Error(string, ...any)                                            {}
func (noopLogger) Fatal(string, ...any)                                            {}
func (noopLogger) SetLevel(schemas.LogLevel)                                       {}
func (noopLogger) SetOutputType(schemas.LoggerOutputType)                          {}
func (noopLogger) LogHTTPRequest(schemas.LogLevel, string) schemas.LogEventBuilder { return nil }

func TestListModelsEntriesCarryOwnerAndDescription(t *testing.T) {
	provider, err := NewTypesafeProvider(&schemas.ProviderConfig{}, noopLogger{})
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}
	key := schemas.Key{Models: []string{"*"}}
	resp, bifrostErr := provider.listModelsByKey(nil, key, &schemas.BifrostListModelsRequest{})
	if bifrostErr != nil {
		t.Fatalf("unexpected error: %v", bifrostErr)
	}
	if len(resp.Data) != len(typesafeModels) {
		t.Fatalf("expected %d models, got %d", len(typesafeModels), len(resp.Data))
	}
	for _, model := range resp.Data {
		if model.OwnedBy == nil || *model.OwnedBy != "typesafe" {
			t.Errorf("model %s missing owned_by: %v", model.ID, model.OwnedBy)
		}
		if model.Description == nil || *model.Description == "" {
			t.Errorf("model %s missing description", model.ID)
		}
		// Pricing intentionally absent here: it backfills from the datasheet in
		// the list-models handler, never from the provider.
		if model.Pricing != nil {
			t.Errorf("model %s carries hardcoded pricing; pricing must come from the datasheet", model.ID)
		}
	}
}

func TestListModelsNativeShape(t *testing.T) {
	name := "Jev (latest)"
	resp := &schemas.BifrostListModelsResponse{
		Data: []schemas.Model{{ID: "typesafe/jev-latest", Name: &name}},
	}
	native := ToTypesafeNativeListModelsResponse(resp)
	if len(native.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(native.Models))
	}
	if native.Models[0].Name != "jev-latest" {
		t.Errorf("native model name = %q, want bare jev-latest", native.Models[0].Name)
	}
	if native.Models[0].Description == "" || native.Models[0].ReleaseDate == "" {
		t.Errorf("catalog metadata not attached: %+v", native.Models[0])
	}
}

func TestToTypesafeNativeError(t *testing.T) {
	errType := "invalid_request_error"
	native := ToTypesafeNativeError(&schemas.BifrostError{
		Error: &schemas.ErrorField{Type: &errType, Message: "question \"q\" has unsupported kind"},
	})
	if native.Detail.ErrorType != "invalid_request_error" {
		t.Errorf("error_type = %q", native.Detail.ErrorType)
	}
	if native.Detail.Message == "" {
		t.Error("message lost")
	}

	// Missing pieces fall back rather than emitting empty fields.
	fallback := ToTypesafeNativeError(nil)
	if fallback.Detail.ErrorType != "api_error" || fallback.Detail.Message == "" {
		t.Errorf("nil error fallback = %+v", fallback.Detail)
	}
}
