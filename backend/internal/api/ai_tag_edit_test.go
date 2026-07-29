package api

import "testing"

func TestEditableAITagBuildsFixedCloseupHierarchy(t *testing.T) {
	tag, err := editableAITag(aiTagMutationRequest{
		Tag: "嘴部特写", CategoryKey: "closeup", SubjectKey: "part",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tag.Tag != "嘴部特写" || tag.CategoryKey != "closeup" || tag.SubjectKey != "part" {
		t.Fatalf("tag = %#v", tag)
	}
	if len(tag.Facets) != 1 || tag.Facets[0].NodeID != "ai:closeup.part.type:嘴部" {
		t.Fatalf("facets = %#v", tag.Facets)
	}
}

func TestEditableAITagRejectsUnknownCloseup(t *testing.T) {
	if _, err := editableAITag(aiTagMutationRequest{
		Tag: "未知部位特写", CategoryKey: "closeup", SubjectKey: "part",
	}); err == nil {
		t.Fatal("expected unknown closeup to be rejected")
	}
}
