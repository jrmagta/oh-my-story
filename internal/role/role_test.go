package role

import "testing"

func TestCan(t *testing.T) {
	want := map[Role][]Action{
		Viewer:    {View},
		Commenter: {View, Comment},
		Editor:    {View, Comment, Edit},
		Admin:     {View, Comment, Edit, ManageMembers, ManageSettings},
		Owner:     {View, Comment, Edit, ManageMembers, ManageSettings, DeleteWorkspace},
	}
	all := []Action{View, Comment, Edit, ManageMembers, ManageSettings, DeleteWorkspace}
	for r, allowed := range want {
		ok := map[Action]bool{}
		for _, a := range allowed {
			ok[a] = true
		}
		for _, a := range all {
			if r.Can(a) != ok[a] {
				t.Errorf("%s.Can(%d) = %v, want %v", r, a, r.Can(a), ok[a])
			}
		}
	}
}

func TestUnknownActionDenied(t *testing.T) {
	if Owner.Can(Action(999)) || Owner.Can(0) {
		t.Error("an action with no minimum role was allowed")
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, r := range []Role{Viewer, Commenter, Editor, Admin, Owner} {
		got, err := Parse(r.String())
		if err != nil || got != r {
			t.Errorf("Parse(%q) = %v, %v", r.String(), got, err)
		}
	}
	if _, err := Parse("superuser"); err == nil {
		t.Error("Parse(superuser) succeeded")
	}
}
